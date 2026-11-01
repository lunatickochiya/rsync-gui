package rsync

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/lunatickochiya/rsync-gui/web/internal/config"
	"github.com/lunatickochiya/rsync-gui/web/internal/location"
)

// Phase is one pass of a transfer.
type Phase int

const (
	PhaseCopy Phase = iota
	PhaseVerify
	PhaseRemove
)

func (p Phase) Label() string {
	switch p {
	case PhaseVerify:
		return "校验"
	case PhaseRemove:
		return "删除源文件"
	default:
		return "复制"
	}
}

// Level is a log severity.
type Level int

const (
	LevelOutput Level = iota
	LevelInfo
	LevelSuccess
	LevelWarning
	LevelError
)

// LogLine is one line in the transfer log.
type LogLine struct {
	Time  time.Time
	Level Level
	Text  string
}

// State is a snapshot of the current transfer, suitable for rendering.
type State struct {
	Phase         string
	Percent       float64
	Indeterminate bool
	Transferred   string
	Rate          string
	ETA           string
	FilesDone     int
	FilesTotal    int
	Speed         int64
	CurrentFile   string
	TransferIndex int
	TransferCount int
	Step          int
	Steps         int
	Elapsed       time.Duration
}

// Summary is the result of a whole run.
type Summary struct {
	Total             int
	OK                int
	Warnings          int
	Failed            int
	Verified          int
	Differences       int
	VerifyFailed      int
	SourcesRemoved    int
	RemovalFailed     int
	SourceDirsRemoved int
	Cancelled         bool
	Aborted           bool
	Duration          time.Duration
	Err               string
}

// Success reports whether every transfer and verification passed.
func (s Summary) Success() bool {
	return !s.Cancelled && !s.Aborted && s.Failed == 0 && s.VerifyFailed == 0 && s.Differences == 0 && s.RemovalFailed == 0
}

// EventKind identifies an event from the runner.
type EventKind int

const (
	EventState EventKind = iota
	EventLog
	EventDone
)

// Event is sent to the UI while a run progresses.
type Event struct {
	Kind    EventKind
	State   State
	Log     LogLine
	Summary *Summary
}

// Start runs a plan in the background and streams events. The channel is
// closed after the final EventDone.
func Start(ctx context.Context, plan *Plan) <-chan Event {
	events := make(chan Event, 256)
	go run(ctx, plan, events)
	return events
}

type runner struct {
	ctx    context.Context
	plan   *Plan
	events chan<- Event
	opts   config.Options
	sum    Summary
	start  time.Time
	state  State
}

func run(ctx context.Context, plan *Plan, events chan<- Event) {
	defer plan.Cleanup()
	defer close(events)
	r := &runner{ctx: ctx, plan: plan, events: events, opts: plan.Options, start: time.Now()}
	r.sum.Total = len(plan.Transfers)
	r.state.TransferCount = len(plan.Transfers)
	r.emitState()

	transfers := plan.Transfers
	for i := range transfers {
		if ctx.Err() != nil {
			r.sum.Cancelled = true
			break
		}
		r.state.TransferIndex = i + 1
		if !r.runTransfer(transfers[i]) {
			break
		}
	}
	r.sum.Duration = time.Since(r.start)
	r.state.Phase = ""
	r.state.Indeterminate = false
	r.emit(Event{Kind: EventDone, State: r.state, Summary: &r.sum})
}

func (r *runner) runTransfer(t Transfer) bool {
	steps := 1
	if r.opts.CanVerify() {
		steps = 2
	}
	if r.opts.VerifiedMove() {
		steps = 3
	}

	// Copy pass.
	if ok := r.runPhase(t, PhaseCopy, 1, steps); !ok {
		r.sum.Failed++
		if r.ctx.Err() != nil {
			r.sum.Cancelled = true
			r.log(LevelWarning, "传输已取消，可能留下部分文件。")
			return false
		}
		if r.opts.CanVerify() {
			r.log(LevelWarning, "传输失败，跳过校验。")
		}
		if r.opts.VerifiedMove() {
			r.log(LevelWarning, "源文件未删除。")
		}
		return true
	}

	if r.opts.VerifiedMove() {
		// The source removal waits for a successful verification.
	} else if r.opts.RemovesSourceDirs() {
		r.cleanupSourceDirs(t)
	}

	if !r.opts.CanVerify() {
		return true
	}

	// Verification pass.
	r.log(LevelInfo, "--- 校验 ---")
	if !r.plan.DestinationRemote {
		r.log(LevelInfo, "将目标文件系统的缓存写入磁盘…")
		if err := syncFilesystem(r.plan.Destination); err != nil {
			r.log(LevelWarning, "写入磁盘缓存失败："+err.Error())
		}
	} else {
		r.log(LevelInfo, "目标是远程目录：校验可能读取服务器内存中的缓存。")
	}
	diffs, ok := r.runVerify(t, 2, steps)
	if r.ctx.Err() != nil {
		r.sum.VerifyFailed++
		r.sum.Cancelled = true
		r.log(LevelWarning, "校验已取消。")
		return false
	}
	if !ok {
		r.sum.VerifyFailed++
		if r.opts.VerifiedMove() {
			r.log(LevelWarning, "校验未通过，源文件未删除。")
		}
		return true
	}
	if diffs > 0 {
		r.sum.Differences += diffs
		if r.opts.Existing != "replace" {
			r.log(LevelError, fmt.Sprintf("校验：目标中有 %d 项与源不同或缺失。未覆盖已存在文件（-u / --ignore-existing），这可能解释差异。", diffs))
		} else {
			r.log(LevelError, fmt.Sprintf("校验：目标中有 %d 项与源不同或缺失。", diffs))
		}
		if r.opts.VerifiedMove() {
			r.log(LevelWarning, "校验未通过，源文件未删除。")
		}
		return true
	}
	r.sum.Verified++
	r.log(LevelSuccess, "校验通过：目标内容与源一致。")

	if !r.opts.VerifiedMove() {
		return true
	}

	// Removal pass of a verified move.
	if ok := r.runPhase(t, PhaseRemove, 3, steps); !ok {
		r.sum.RemovalFailed++
		if r.ctx.Err() != nil {
			r.sum.Cancelled = true
			r.log(LevelWarning, "删除源文件已取消，部分源文件可能已删除。")
			return false
		}
		return true
	}
	r.sum.SourcesRemoved++
	r.log(LevelSuccess, "源文件已删除。")
	if r.opts.RemovesSourceDirs() {
		r.cleanupSourceDirs(t)
	}
	return true
}

// runPhase executes one rsync pass and returns whether it succeeded (24 counts
// as success with a warning).
func (r *runner) runPhase(t Transfer, phase Phase, step, steps int) bool {
	cmdArgs := r.plan.CommandFor(t, phase)
	if len(cmdArgs) == 0 {
		return false
	}
	label := fmt.Sprintf("%s %d/%d", phase.Label(), step, steps)
	if r.opts.VerifiedMove() && phase == PhaseCopy {
		label = fmt.Sprintf("复制（校验后删除源）%d/%d", step, steps)
	}
	if len(r.plan.Transfers) > 1 {
		label = fmt.Sprintf("%s · %s", label, t.Description)
	}
	r.state.Phase = label
	r.state.Step = step
	r.state.Steps = steps
	r.state.Percent = 0
	r.state.Indeterminate = true
	r.state.Transferred, r.state.Rate, r.state.ETA = "", "", ""
	r.state.FilesDone, r.state.FilesTotal = 0, 0
	r.state.Speed = 0
	r.state.CurrentFile = ""
	r.state.Elapsed = time.Since(r.start)
	r.emitState()
	r.log(LevelInfo, "--- "+label+" ---")
	r.log(LevelInfo, "$ "+config.JoinCommand(cmdArgs[0], cmdArgs[1:]))

	code, cancelled, err := r.runProcess(cmdArgs[0], cmdArgs[1:])
	if err != nil {
		r.log(LevelError, err.Error())
		return false
	}
	if cancelled {
		return false
	}
	switch {
	case code == 0:
		if phase == PhaseCopy {
			r.sum.OK++
			r.log(LevelSuccess, "传输完成。")
		} else if phase == PhaseRemove {
			r.log(LevelSuccess, "删除源文件完成。")
		}
		return true
	case code == 24:
		if phase == PhaseCopy {
			r.sum.Warnings++
			r.log(LevelWarning, "传输完成，但有源文件在传输过程中消失（退出码 24）。")
		} else {
			r.log(LevelWarning, "完成，但有源文件在过程中消失（退出码 24）。")
		}
		return true
	default:
		desc := exitCodeDescription(code)
		if phase == PhaseCopy {
			r.log(LevelError, fmt.Sprintf("传输失败（退出码 %d：%s）", code, desc))
		} else if phase == PhaseVerify {
			r.log(LevelError, fmt.Sprintf("校验失败（退出码 %d：%s）", code, desc))
		} else {
			r.log(LevelError, fmt.Sprintf("删除源文件失败（退出码 %d：%s）", code, desc))
		}
		return false
	}
}

func (r *runner) runVerify(t Transfer, step, steps int) (int, bool) {
	cmdArgs := r.plan.CommandFor(t, PhaseVerify)
	label := fmt.Sprintf("%s %d/%d", PhaseVerify.Label(), step, steps)
	if len(r.plan.Transfers) > 1 {
		label = fmt.Sprintf("%s · %s", label, t.Description)
	}
	r.state.Phase = label
	r.state.Step = step
	r.state.Steps = steps
	r.state.Indeterminate = true
	r.state.Percent = 0
	r.state.Transferred, r.state.Rate, r.state.ETA = "", "", ""
	r.state.FilesDone, r.state.FilesTotal = 0, 0
	r.state.Speed = 0
	r.state.CurrentFile = ""
	r.emitState()
	r.log(LevelInfo, "--- "+label+" ---")
	r.log(LevelInfo, "$ "+config.JoinCommand(cmdArgs[0], cmdArgs[1:]))

	diffs := 0
	res := r.runProcessFunc(cmdArgs[0], cmdArgs[1:], func(line string) {
		if name, ok := ParseDifferenceLine(line); ok {
			diffs++
			if diffs <= 500 {
				r.log(LevelWarning, "差异："+name)
			} else if diffs == 501 {
				r.log(LevelWarning, "差异过多，后续只计数。")
			}
			return
		}
		r.logLine(line)
	})
	if res.err != nil {
		r.log(LevelError, res.err.Error())
		return diffs, false
	}
	if res.cancelled {
		return diffs, false
	}
	if res.code != 0 {
		r.log(LevelError, fmt.Sprintf("校验失败（退出码 %d：%s）", res.code, exitCodeDescription(res.code)))
		return diffs, false
	}
	return diffs, true
}

func (r *runner) cleanupSourceDirs(t Transfer) {
	if !r.opts.RemovesSourceDirs() || len(t.LocalSourceDirs) == 0 {
		return
	}
	for _, dir := range t.LocalSourceDirs {
		removed, left := removeEmptyDirectories(dir, r.opts.ContentsOnly)
		r.sum.SourceDirsRemoved += removed
		if removed > 0 {
			r.log(LevelInfo, fmt.Sprintf("已删除 %d 个空的源目录（%s）", removed, location.DisplayPath(dir)))
		}
		for _, path := range left {
			r.log(LevelWarning, "源目录非空，已保留："+location.DisplayPath(path))
		}
	}
}

type processResult struct {
	code      int
	cancelled bool
	err       error
}

func (r *runner) runProcess(program string, args []string) (int, bool, error) {
	res := r.runProcessFunc(program, args, r.logLine)
	return res.code, res.cancelled, res.err
}

func (r *runner) runProcessFunc(program string, args []string, onLine func(string)) processResult {
	cmd := exec.Command(program, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C.UTF-8", "LANG=C.UTF-8")
	cmd.Stdin = nil // never let ssh prompt inside the web server
	setProcessGroup(cmd)

	pr, pw, err := os.Pipe()
	if err != nil {
		return processResult{code: -1, err: fmt.Errorf("无法创建输出管道：%w", err)}
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		return processResult{code: -1, err: fmt.Errorf("无法启动 %q：%w", program, err)}
	}
	pw.Close()

	done := make(chan struct{})
	go func() {
		select {
		case <-r.ctx.Done():
			interruptProcess(cmd)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				killProcess(cmd)
			}
		case <-done:
		}
	}()

	tracker := newProgressTracker(r.opts)
	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	scanner.Split(splitLines)
	lastFile := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		line = stripProgressPrefix(line)
		if p, ok := ParseProgressLine(line, HumanLevel(args)); ok {
			r.state.Indeterminate = false
			r.state.Transferred = p.Transferred
			r.state.Rate = p.Rate
			r.state.ETA = p.ETA
			r.state.Speed = p.Speed
			if p.HasTotal {
				r.state.FilesTotal = p.Total
				r.state.FilesDone = p.Total - p.Remaining
			}
			r.state.Percent = tracker.percent(p)
			r.state.Elapsed = time.Since(r.start)
			r.emitState()
			continue
		}
		if phaseIsVerify(r.state.Phase) {
			onLine(line)
			continue
		}
		onLine(line)
		if !isStatusLine(line) && !strings.HasPrefix(line, "rsync:") && !strings.HasPrefix(line, "rsync error:") && !strings.HasPrefix(line, "rsync warning:") {
			name := line
			if idx := strings.Index(line, " -> "); idx > 0 {
				name = line[:idx]
			}
			if name != lastFile {
				lastFile = name
				r.state.CurrentFile = name
				r.state.Elapsed = time.Since(r.start)
				r.emitState()
			}
		}
	}
	scanErr := scanner.Err()
	pr.Close()
	waitErr := cmd.Wait()
	close(done)
	r.state.CurrentFile = ""
	r.state.Speed = 0
	r.state.Transferred = ""
	r.state.Rate = ""
	r.state.ETA = ""
	r.state.Elapsed = time.Since(r.start)
	r.emitState()

	res := processResult{code: 0}
	if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			res.code = ee.ExitCode()
		} else {
			res.err = fmt.Errorf("rsync 运行失败：%w", waitErr)
			return res
		}
	}
	if scanErr != nil {
		res.err = fmt.Errorf("读取 rsync 输出失败：%w", scanErr)
	}
	res.cancelled = r.ctx.Err() != nil
	return res
}

func isStatusLine(line string) bool {
	prefixes := []string{
		"sent ", "total size is ", "created directory", "sending incremental file list",
		"receiving incremental file list", "building file list", "file list ...",
		"opening connection", "opening tcp connection", "done", "deleting ",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func phaseIsVerify(phase string) bool {
	return strings.HasPrefix(phase, "校验")
}

func (r *runner) logLine(line string) {
	level := LevelOutput
	switch {
	case strings.HasPrefix(line, "rsync: ") || strings.HasPrefix(line, "rsync error:"):
		level = LevelError
	case strings.HasPrefix(line, "rsync warning: ") || strings.HasPrefix(line, "file has vanished: "):
		level = LevelWarning
	}
	r.log(level, line)
}

func (r *runner) log(level Level, text string) {
	r.emit(Event{Kind: EventLog, Log: LogLine{Time: time.Now(), Level: level, Text: text}})
}

func (r *runner) emitState() {
	r.emit(Event{Kind: EventState, State: r.state})
}

func (r *runner) emit(ev Event) {
	select {
	case r.events <- ev:
	default:
		// Never drop log lines or the final summary. Dropping an intermediate
		// progress snapshot is harmless because the next one is complete.
		if ev.Kind == EventState {
			return
		}
		r.events <- ev
	}
}

type progressTracker struct {
	opts      config.Options
	lastDone  int
	lastTotal int
}

func newProgressTracker(opts config.Options) *progressTracker {
	return &progressTracker{opts: opts}
}

func (t *progressTracker) percent(p Progress) float64 {
	if p.HasTotal {
		t.lastTotal = p.Total
		t.lastDone = p.Total - p.Remaining
	}
	var fraction float64
	switch t.opts.Progress {
	case "overall":
		fraction = float64(p.Percent) / 100
	case "per-file":
		if t.lastTotal > 0 {
			current := 0.0
			if !p.HasTotal {
				current = float64(p.Percent) / 100
			}
			denom := t.lastTotal
			if denom < t.lastDone+1 {
				denom = t.lastDone + 1
			}
			fraction = (float64(t.lastDone) + current) / float64(denom)
		} else {
			fraction = float64(p.Percent) / 100
		}
	default:
		return 0
	}
	if fraction < 0 {
		return 0
	}
	if fraction > 1 {
		return 100
	}
	return fraction * 100
}

// stripProgressPrefix repairs lines where rsync appended an error to a
// progress line without a separator, which happens on cancellation.
func stripProgressPrefix(line string) string {
	for _, marker := range []string{"rsync error:", "rsync warning:", "rsync: ["} {
		if idx := strings.Index(line, marker); idx > 0 {
			prefix := line[:idx]
			if strings.Contains(prefix, "%") && strings.Contains(prefix, "/s") {
				return line[idx:]
			}
		}
	}
	return line
}

// splitLines splits rsync output on both \r and \n.
func splitLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF {
		if len(data) == 0 {
			return 0, nil, nil
		}
		return len(data), data, nil
	}
	return 0, nil, nil
}

func exitCodeDescription(code int) string {
	switch code {
	case 0:
		return "成功"
	case 1:
		return "语法或用法错误"
	case 2:
		return "协议不兼容"
	case 3:
		return "选择输入/输出文件或目录时出错"
	case 4:
		return "不支持所请求的操作"
	case 5:
		return "启动客户端-服务器协议时出错"
	case 10:
		return "套接字 I/O 错误"
	case 11:
		return "文件 I/O 错误"
	case 12:
		return "rsync 协议数据流错误"
	case 14:
		return "IPC 错误"
	case 20:
		return "被中断（SIGUSR1 或 SIGINT）"
	case 22:
		return "内存分配错误"
	case 23:
		return "传输部分失败"
	case 24:
		return "部分源文件在传输中消失"
	case 25:
		return "--max-delete 限制阻止了删除"
	case 30:
		return "数据发送/接收超时"
	case 35:
		return "等待守护进程连接超时"
	case 255:
		return "远程 shell（ssh）错误"
	default:
		return "错误"
	}
}
