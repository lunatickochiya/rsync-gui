package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/lunatickochiya/rsync-gui/tui/internal/config"
	"github.com/lunatickochiya/rsync-gui/tui/internal/location"
	"github.com/lunatickochiya/rsync-gui/tui/internal/rsync"
)

type screen int

const (
	screenSetup screen = iota
	screenRun
)

type focusZone int

const (
	focusSources focusZone = iota
	focusOptions
)

type modalKind int

const (
	modalNone modalKind = iota
	modalInput
	modalBulk
	modalPresets
	modalConfirm
	modalHelp
	modalMessage
	modalCommand
)

type modalState struct {
	kind         modalKind
	title        string
	subtitle     string
	field        string
	action       string
	body         string
	hint         string
	confirmLabel string
	items        []config.Preset
	cursor       int
	scroll       int
	command      string
	err          string
}

type tickMsg time.Time

type runEventMsg struct {
	event rsync.Event
}

type runClosedMsg struct{}

// undoProgressMsg is a live status of a running undo.
type undoProgressMsg struct {
	permille int
	status   string
}

// undoDoneMsg ends an undo.
type undoDoneMsg struct {
	record rsync.UndoRecord
	result rsync.UndoResult
}

// Model is the root Bubble Tea model.
type Model struct {
	width  int
	height int

	screen screen
	focus  focusZone

	opts         config.Options
	sources      []string
	dest         string
	sourceCursor int
	sourceScroll int
	optionCursor int
	optionScroll int
	activePreset string

	presets []config.Preset
	cfg     config.AppConfig

	modal       modalState
	hits        uiHits
	input       textinput.Model
	bulk        textarea.Model
	pendingPlan *rsync.Plan

	completions    []string
	completionBase string
	completionIdx  int
	recentIdx      int

	toast      string
	toastBad   bool
	toastUntil time.Time

	frame int

	running    bool
	done       bool
	cancelling bool
	plan       *rsync.Plan
	cancel     context.CancelFunc
	events     <-chan rsync.Event
	state      rsync.State
	logs       []rsync.LogLine
	logScroll  int
	summary    *rsync.Summary
	commands   []string
	speeds     []float64
	startedAt  time.Time
	elapsed    time.Duration
	autoRun    bool

	// Undo of the last finished run (see rsync.UndoRecord): nil when there is
	// none. undoEvents streams the progress of a running undo.
	undoRecord  *rsync.UndoRecord
	undoResult  *rsync.UndoResult
	undoRunning bool
	undoEvents  <-chan undoMsg

	rsyncVersion string
	wsl          bool
}

// New creates the TUI model. autoRun starts immediately when sources and a
// destination are already supplied.
func New(opts config.Options, sources []string, dest string, cfg config.AppConfig, presetID string, autoRun bool) *Model {
	m := &Model{
		width:        100,
		height:       30,
		opts:         opts,
		sources:      append([]string(nil), sources...),
		dest:         dest,
		activePreset: presetID,
		optionCursor: 1,
		cfg:          cfg,
		presets:      config.AvailablePresets(),
		recentIdx:    -1,
		autoRun:      autoRun,
		wsl:          isWSL(),
	}
	m.input = textinput.New()
	m.input.Prompt = "› "
	m.input.PromptStyle = styAccent
	m.input.TextStyle = styText
	m.input.PlaceholderStyle = styFaint
	m.input.Cursor.Style = styAccent
	m.input.CharLimit = 8192
	m.input.Width = 60

	m.bulk = textarea.New()
	m.bulk.Placeholder = "每行一个源路径…\n支持 /path、~/path、user@host:/path、sftp://host/path"
	m.bulk.Prompt = ""
	m.bulk.CharLimit = 128 * 1024
	m.bulk.ShowLineNumbers = false
	m.bulk.SetWidth(64)
	m.bulk.SetHeight(8)
	m.bulk.FocusedStyle.CursorLine = styPanel
	m.bulk.FocusedStyle.Base = styText
	m.bulk.BlurredStyle.Base = styMuted

	m.rsyncVersion = detectRsyncVersion(opts.Program)
	m.clampSourceCursor()

	// A copy run of the previous session that can still be undone: the record
	// file is inside the destination, only the pointer is remembered here.
	if cfg.UndoRecordPath != "" {
		if rec, ok := rsync.UndoLoad(rsync.UnescapeForRecord(cfg.UndoRecordPath)); ok {
			deletions, _, backupExists := rsync.UndoCounts(rec)
			if backupExists || deletions > 0 {
				m.undoRecord = &rec
				m.setToast("上次的复制仍可撤回（按 u）："+location.DisplayPath(rec.Destination), false)
			} else {
				_, _ = rsync.UndoDiscard(rec) // nothing usable left
				m.setUndoRecord(nil)
			}
		} else {
			m.setUndoRecord(nil)
		}
	}
	return m
}

// Init starts the UI.
func (m *Model) Init() tea.Cmd {
	var cmd tea.Cmd
	if m.autoRun {
		m.autoRun = false
		cmd = m.requestStart(true)
	}
	return tea.Batch(tickCmd(), cmd)
}

// SetAutoRun asks the model to start as soon as possible.
func (m *Model) SetAutoRun() { m.autoRun = true }

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.modal.kind == modalInput {
			m.input.Width = clamp(m.width-12, 20, 84)
		}
		if m.modal.kind == modalBulk {
			m.bulk.SetWidth(clamp(m.width-12, 30, 90))
			m.bulk.SetHeight(clamp(m.height-14, 4, 14))
		}
		return m, nil
	case tickMsg:
		m.frame++
		if m.toast != "" && time.Now().After(m.toastUntil) {
			m.toast = ""
		}
		if m.running {
			m.elapsed = time.Since(m.startedAt)
		}
		return m, tickCmd()
	case runEventMsg:
		return m.handleRunEvent(msg.event)
	case runClosedMsg:
		if m.running {
			m.running = false
			m.done = true
			m.summary = &rsync.Summary{Aborted: true, Err: "传输进程意外结束"}
			m.log(rsync.LevelError, "传输进程意外结束。")
		}
		return m, nil
	case undoProgressMsg:
		if !m.undoRunning {
			return m, nil
		}
		m.state.Indeterminate = false
		m.state.Percent = float64(msg.permille) / 10
		if msg.status != "" {
			m.state.CurrentFile = msg.status
		}
		m.elapsed = time.Since(m.startedAt)
		return m, waitUndo(m.undoEvents)
	case undoDoneMsg:
		return m.finishUndo(msg.record, msg.result)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	case tea.KeyMsg:
		if m.modal.kind != modalNone {
			return m.handleModalKey(msg)
		}
		if m.screen == screenRun {
			return m.handleRunKey(msg)
		}
		return m.handleSetupKey(msg)
	}
	return m, nil
}

func (m *Model) handleRunEvent(ev rsync.Event) (tea.Model, tea.Cmd) {
	switch ev.Kind {
	case rsync.EventState:
		m.state = ev.State
		m.elapsed = ev.State.Elapsed
		if ev.State.Speed > 0 {
			m.speeds = append(m.speeds, float64(ev.State.Speed))
			if len(m.speeds) > 240 {
				m.speeds = m.speeds[len(m.speeds)-240:]
			}
		}
	case rsync.EventLog:
		m.logs = append(m.logs, ev.Log)
		if len(m.logs) > 5000 {
			m.logs = m.logs[len(m.logs)-5000:]
		}
	case rsync.EventUndoAvailable:
		m.undoRecord = ev.UndoRecord
		m.undoResult = nil
		deletions, restorations, _ := rsync.UndoCounts(*ev.UndoRecord)
		m.log(rsync.LevelInfo, fmt.Sprintf(
			"这次复制可以撤回：将删除新建的 %d 项，还原被覆盖或删除的 %d 项。按 u 撤回。",
			deletions, restorations))
	case rsync.EventDone:
		m.running = false
		m.done = true
		m.cancelling = false
		m.summary = ev.Summary
		m.toast = ""
		m.state.Indeterminate = false
		m.state.CurrentFile = ""
		m.state.Transferred = ""
		m.state.Rate = ""
		m.state.ETA = ""
		m.state.Speed = 0
		if ev.Summary != nil && !ev.Summary.Cancelled && !ev.Summary.Aborted && ev.Summary.Failed == 0 {
			m.state.Percent = 100
			if m.state.FilesTotal > 0 {
				m.state.FilesDone = m.state.FilesTotal
			}
		}
		m.plan = nil
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		return m, nil
	}
	if m.running {
		return m, waitEvent(m.events)
	}
	return m, nil
}

func (m *Model) handleSetupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.persist()
		return m, tea.Quit
	case "?":
		m.openHelp()
	case "tab", "shift+tab":
		if m.focus == focusSources {
			m.focus = focusOptions
		} else {
			m.focus = focusSources
		}
	case "s":
		m.focus = focusSources
	case "o":
		m.focus = focusOptions
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "pgup":
		m.moveCursor(-8)
	case "pgdown":
		m.moveCursor(8)
	case "home", "g":
		m.moveCursor(-100000)
	case "end", "G":
		m.moveCursor(100000)
	case "a":
		m.openSourceInput()
	case "A":
		m.openBulkInput()
	case "e":
		if m.focus == focusSources {
			m.openSourceEdit()
		}
	case "x", "delete":
		m.removeSource()
	case "c":
		if len(m.sources) > 0 {
			m.modal = modalState{kind: modalConfirm, title: "清空源列表？", body: "只会清空当前列表，不会删除任何文件。", action: "clear-sources", confirmLabel: "清空"}
		}
	case "d":
		m.openDestInput()
	case "p":
		m.openPresetPicker()
	case "r", "ctrl+r":
		return m, m.requestStart(false)
	case "v":
		m.openCommand()
	case "I":
		m.openInput("import-settings", "导入设置", "载入 rsync-gui 兼容的 JSON 设置文件", "")
	case "E":
		home, _ := os.UserHomeDir()
		m.openInput("export-settings", "导出设置", "将当前选项保存为 rsync-gui 兼容的 JSON", filepath.Join(home, "rsync-tui-settings.json"))
	case "t":
		m.opts.DryRun = !m.opts.DryRun
		m.markCustom()
		if m.opts.DryRun {
			m.setToast("试运行已开启：不会修改文件", false)
		} else {
			m.setToast("试运行已关闭", false)
		}
	case "u":
		if m.undoRecord != nil && !m.running && !m.undoRunning {
			return m, m.requestUndo()
		}
	case "esc":
		m.toast = ""
	case "space", "enter":
		if m.focus == focusSources {
			m.openSourceEdit()
		} else {
			m.toggleCurrentOption(msg.String() == "enter")
		}
	}
	return m, nil
}

func (m *Model) handleRunKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.running {
			m.cancelRun()
		} else {
			m.persist()
			return m, tea.Quit
		}
	case "q":
		if !m.running {
			m.persist()
			return m, tea.Quit
		}
	case "esc":
		if m.running {
			m.modal = modalState{
				kind: modalConfirm, title: "取消传输？",
				body: "已经复制的文件会保留；正在传输的文件可能不完整。", action: "cancel-run", confirmLabel: "取消传输",
			}
		} else {
			m.backToSetup()
		}
	case "enter":
		if !m.running {
			m.backToSetup()
		}
	case "up", "k":
		m.scrollLog(1)
	case "down", "j":
		m.scrollLog(-1)
	case "pgup":
		m.scrollLog(10)
	case "pgdown":
		m.scrollLog(-10)
	case "home":
		m.logScroll = len(m.logs)
	case "end":
		m.logScroll = 0
	case "?":
		m.openHelp()
	case "v":
		if len(m.commands) > 0 {
			m.modal = modalState{kind: modalCommand, title: "执行的命令", command: strings.Join(m.commands, "\n\n"), hint: "↑↓ 滚动 · enter 关闭"}
		}
	case "u":
		if !m.running && m.undoRecord != nil && !m.undoRunning {
			return m, m.requestUndo()
		}
	case "r":
		if !m.running {
			return m, m.requestStart(false)
		}
	}
	return m, nil
}

func (m *Model) requestStart(auto bool) tea.Cmd {
	plan, err := rsync.BuildPlan(m.opts, m.sources, m.dest)
	if err != nil {
		m.openMessage("无法开始传输", err.Error())
		return nil
	}
	destructive := plan.Options.DeleteExtraneous || plan.Options.RemoveSourceFiles
	if destructive {
		m.pendingPlan = plan
		var lines []string
		if plan.Options.DeleteExtraneous {
			lines = append(lines, "• 将删除目标目录中源里不存在的文件（--delete）。")
		}
		if plan.Options.RemoveSourceFiles {
			if plan.Options.VerifiedMove() {
				lines = append(lines, "• 先复制并校验，通过后删除源文件。")
			} else {
				lines = append(lines, "• 传输完成后删除源文件（--remove-source-files）。")
			}
			if plan.Options.RemoveSourceDirs {
				lines = append(lines, "• 并删除源中变空的目录。")
			}
		}
		m.modal = modalState{
			kind: modalConfirm, title: "确认开始？", body: strings.Join(lines, "\n"),
			action: "start-plan", confirmLabel: "开始",
		}
		return nil
	}
	return m.startPlan(plan)
}

func (m *Model) startPlan(plan *rsync.Plan) tea.Cmd {
	m.pendingPlan = nil
	m.plan = plan
	m.opts = plan.Options
	// One undo at a time: the data of the previous run is removed before this one
	// starts, so that rsync does not see it as part of the destination.
	m.discardUndoRecord()
	m.undoResult = nil
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.events = rsync.Start(ctx, plan)
	m.running = true
	m.done = false
	m.cancelling = false
	m.summary = nil
	m.logs = nil
	m.speeds = nil
	m.logScroll = 0
	m.state = rsync.State{TransferCount: len(plan.Transfers)}
	m.startedAt = time.Now()
	m.elapsed = 0
	m.screen = screenRun
	m.modal = modalState{}
	m.commands = planCommands(plan)
	m.cfg.Options = m.opts
	m.cfg.Sources = append([]string(nil), m.sources...)
	m.cfg.RememberDestination(m.dest)
	_ = m.cfg.Save()
	return waitEvent(m.events)
}

func (m *Model) cancelRun() {
	if !m.running || m.cancel == nil || m.cancelling {
		return
	}
	m.cancelling = true
	if m.undoRunning {
		m.cancel()
		return
	}
	m.cancel()
	m.log(rsync.LevelWarning, "正在取消…")
	m.setToast("正在取消，等待 rsync 退出…", true)
}

// undoMsg is a progress or completion message of a running undo.
type undoMsg interface{}

func waitUndo(ch <-chan undoMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// requestUndo asks for confirmation, then undo the last finished copy.
func (m *Model) requestUndo() tea.Cmd {
	if m.undoRecord == nil {
		return nil
	}
	rec := *m.undoRecord
	deletions, restorations, backupExists := rsync.UndoCounts(rec)
	if !backupExists && deletions == 0 {
		m.openMessage("撤回复制", "已没有可撤回的内容："+location.DisplayPath(rec.UndoDir())+" 中的撤回数据已不存在。")
		m.clearUndoRecord()
		return nil
	}
	if deletions == 0 && restorations == 0 {
		m.clearUndoRecord() // nothing changed, or it was undone already
		return nil
	}
	body := fmt.Sprintf("把 %s 恢复到复制前的状态？\n\n"+
		"• 删除这次新建的条目：%d 项\n"+
		"• 还原被覆盖或删除的旧文件：%d 项\n\n"+
		"源不会被改动。此操作无法重做。",
		location.DisplayPath(rec.Destination), deletions, restorations)
	m.modal = modalState{
		kind: modalConfirm, title: "撤回这次复制？", body: body,
		action: "undo-copy", confirmLabel: "撤回",
	}
	return nil
}

func (m *Model) startUndo() tea.Cmd {
	if m.undoRecord == nil || m.running || m.undoRunning {
		return nil
	}
	rec := *m.undoRecord
	m.modal = modalState{}
	m.screen = screenRun
	m.running = true
	m.done = false
	m.cancelling = false
	m.summary = nil
	m.undoResult = nil
	m.undoRunning = true
	m.speeds = nil
	m.logScroll = 0
	m.state = rsync.State{Phase: "撤回", Indeterminate: true}
	m.startedAt = time.Now()
	m.elapsed = 0
	m.log(rsync.LevelInfo, "--- 撤回：恢复 "+location.DisplayPath(rec.Destination)+" ---")

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	ch := make(chan undoMsg, 64)
	m.undoEvents = ch
	go func() {
		defer close(ch)
		result := rsync.UndoExecute(ctx, rec, func(permille int, status string) {
			select {
			case ch <- undoProgressMsg{permille: permille, status: status}:
			case <-ctx.Done():
			}
		})
		select {
		case ch <- undoDoneMsg{record: rec, result: result}:
		case <-ctx.Done():
		}
	}()
	return waitUndo(ch)
}

func (m *Model) finishUndo(rec rsync.UndoRecord, result rsync.UndoResult) (tea.Model, tea.Cmd) {
	m.running = false
	m.done = true
	m.cancelling = false
	m.undoRunning = false
	m.cancel = nil
	m.state.Indeterminate = false
	m.state.CurrentFile = ""
	m.state.Percent = 100
	m.elapsed = time.Since(m.startedAt)
	for _, problem := range result.Problems {
		m.log(rsync.LevelWarning, problem)
	}
	if result.Cancelled {
		m.log(rsync.LevelWarning, "撤回已取消。目标目前处于两种状态之间；再按 u 会从这里继续。")
	} else if result.Failed == 0 {
		m.log(rsync.LevelSuccess, fmt.Sprintf("已撤回：删除 %d 项，还原 %d 项。", result.Deleted, result.Restored))
	} else {
		m.log(rsync.LevelError, fmt.Sprintf("已撤回：删除 %d 项，还原 %d 项，%d 项未能撤回。",
			result.Deleted, result.Restored, result.Failed))
	}
	if result.LeftInBackupDir != "" {
		m.log(rsync.LevelWarning, "无法还原的旧版本仍保存在 "+location.DisplayPath(filepath.Join(rec.Destination, result.LeftInBackupDir))+"。")
	}
	m.undoResult = &result
	if result.OK() {
		m.clearUndoRecord() // the backup dir is empty and removed now
	} else {
		// Keep the record: another undo finishes the rest.
		m.setUndoRecord(&rec)
	}
	return m, nil
}

// setUndoRecord remembers a run that can be undone (the pointer to the record
// file in the config keeps it across a restart of the TUI).
func (m *Model) setUndoRecord(rec *rsync.UndoRecord) {
	m.undoRecord = rec
	if rec != nil {
		// The path may contain bytes that are not valid UTF-8; the config is
		// JSON, so it is stored in the escaped form (see rsync.EscapeForRecord).
		m.cfg.UndoRecordPath = rsync.EscapeForRecord(rec.RecordPath())
	} else {
		m.cfg.UndoRecordPath = ""
	}
	_ = m.cfg.Save()
}

func (m *Model) clearUndoRecord() {
	m.undoRecord = nil
	m.setUndoRecord(nil)
}

// discardUndoRecord deletes the undo data of the previous run before a new one
// starts, so that rsync does not see it as part of the destination.
func (m *Model) discardUndoRecord() {
	if m.undoRecord == nil {
		return
	}
	rec := *m.undoRecord
	if problems, ok := rsync.UndoDiscard(rec); !ok {
		for _, problem := range problems {
			m.log(rsync.LevelWarning, problem)
		}
		m.log(rsync.LevelWarning, "上次运行的撤回数据未能完全删除："+location.DisplayPath(rec.UndoDir()))
	}
	m.clearUndoRecord()
}

func planCommands(plan *rsync.Plan) []string {
	var out []string
	for _, tr := range plan.Transfers {
		cmd := plan.CommandFor(tr, rsync.PhaseCopy)
		out = append(out, config.JoinCommand(cmd[0], cmd[1:]))
		if plan.Options.CanVerify() {
			cmd = plan.CommandFor(tr, rsync.PhaseVerify)
			out = append(out, config.JoinCommand(cmd[0], cmd[1:]))
		}
		if plan.Options.VerifiedMove() {
			cmd = plan.CommandFor(tr, rsync.PhaseRemove)
			out = append(out, config.JoinCommand(cmd[0], cmd[1:]))
		}
	}
	return out
}

func (m *Model) backToSetup() {
	m.screen = screenSetup
	m.running = false
	m.done = false
	m.summary = nil
	m.state = rsync.State{}
	m.logs = nil
	m.speeds = nil
	m.logScroll = 0
	m.modal = modalState{}
	m.plan = nil
	m.commands = nil
	m.opts.FilesFrom = ""
	m.opts.FilesFromZero = false
	m.opts.ModifyWindow = 0
	m.focus = focusSources
}

func (m *Model) log(level rsync.Level, text string) {
	m.logs = append(m.logs, rsync.LogLine{Time: time.Now(), Level: level, Text: text})
}

func (m *Model) scrollLog(delta int) {
	if len(m.logs) == 0 {
		return
	}
	m.logScroll = clamp(m.logScroll+delta, 0, len(m.logs))
}

func (m *Model) setToast(text string, bad bool) {
	m.toast = text
	m.toastBad = bad
	m.toastUntil = time.Now().Add(3 * time.Second)
}

func (m *Model) persist() {
	m.cfg.Options = m.opts
	m.cfg.Sources = append([]string(nil), m.sources...)
	if m.dest != "" {
		m.cfg.RememberDestination(m.dest)
	}
	_ = m.cfg.Save()
}

func (m *Model) markCustom() {
	if m.activePreset != "" {
		m.activePreset = ""
	}
}

func (m *Model) clampSourceCursor() {
	if len(m.sources) == 0 {
		m.sourceCursor = 0
	} else {
		m.sourceCursor = clamp(m.sourceCursor, 0, len(m.sources)-1)
	}
}

func (m *Model) moveCursor(delta int) {
	if m.focus == focusSources {
		if len(m.sources) == 0 {
			return
		}
		m.sourceCursor = clamp(m.sourceCursor+delta, 0, len(m.sources)-1)
		return
	}
	rows := optionRows()
	idx := m.optionCursor
	if idx+delta >= len(rows) {
		idx = len(rows) - 1
		for idx > 0 && rows[idx].kind == rowHeader {
			idx--
		}
	} else if idx+delta < 0 {
		idx = 0
		for idx < len(rows) && rows[idx].kind == rowHeader {
			idx++
		}
	} else {
		idx += delta
		for idx >= 0 && idx < len(rows) && rows[idx].kind == rowHeader {
			idx += sign(delta)
		}
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(rows) {
		idx = len(rows) - 1
	}
	m.optionCursor = idx
}

func (m *Model) openSourceInput() {
	m.openInput("source", "添加源", "本地路径、user@host:/path 或 sftp://host/path", "")
}

func (m *Model) openSourceEdit() {
	if len(m.sources) == 0 {
		m.openSourceInput()
		return
	}
	m.openInput("source-edit", "编辑源", "修改路径后按 enter 保存", m.sources[m.sourceCursor])
}

func (m *Model) removeSource() {
	if m.focus != focusSources || len(m.sources) == 0 {
		return
	}
	m.sources = append(m.sources[:m.sourceCursor], m.sources[m.sourceCursor+1:]...)
	m.clampSourceCursor()
	m.setToast("已移除源", false)
}

func (m *Model) openDestInput() {
	m.openInput("dest", "目标目录", "本地目录会自动创建；也支持 user@host:/path", m.dest)
}

func (m *Model) openBulkInput() {
	m.modal = modalState{kind: modalBulk, title: "批量添加源", hint: "ctrl+s 添加 · esc 取消"}
	m.bulk.SetValue("")
	m.bulk.SetWidth(clamp(m.width-12, 30, 90))
	m.bulk.SetHeight(clamp(m.height-14, 4, 14))
	m.bulk.Focus()
}

func (m *Model) openInput(field, title, subtitle, value string) {
	m.modal = modalState{kind: modalInput, field: field, title: title, subtitle: subtitle}
	m.input.SetValue(value)
	m.input.CursorEnd()
	m.input.Focus()
	m.input.Width = clamp(m.width-12, 20, 84)
	m.completions = nil
	m.completionBase = ""
	m.completionIdx = -1
	m.recentIdx = -1
	if field == "dest" {
		m.input.Placeholder = "例如 ~/backup 或 nas:/volume1/backup"
	} else if field == "source" || field == "source-edit" {
		m.input.Placeholder = "例如 ~/Photos"
	} else {
		m.input.Placeholder = ""
	}
}

func (m *Model) openPresetPicker() {
	items := m.presets
	if len(items) == 0 {
		m.setToast("没有可用预设", true)
		return
	}
	m.modal = modalState{kind: modalPresets, title: "选择预设", items: items, hint: "enter 应用 · esc 取消"}
	m.modal.cursor = 0
	for i, p := range items {
		if p.ID == m.activePreset {
			m.modal.cursor = i
			break
		}
	}
}

func (m *Model) openHelp() {
	m.modal = modalState{kind: modalHelp, title: "快捷键", hint: "↑↓ 滚动 · esc 关闭"}
}

func (m *Model) openMessage(title, body string) {
	m.modal = modalState{kind: modalMessage, title: title, body: body, hint: "enter 关闭"}
}

func (m *Model) openCommand() {
	m.modal = modalState{kind: modalCommand, title: "命令预览", command: m.previewCommand(), hint: "enter 关闭 · 复制后可在终端运行"}
}

func (m *Model) previewCommand() string {
	if len(m.sources) == 0 || strings.TrimSpace(m.dest) == "" {
		return "填写源和目标后会显示 rsync 命令。"
	}
	dest, err := location.Parse(m.dest)
	if err != nil {
		return "目标无效：" + err.Error()
	}
	port := dest.Port
	if port == 0 {
		for _, raw := range m.sources {
			if loc, err := location.Parse(raw); err == nil && loc.Kind == location.Remote && loc.Port > 0 {
				port = loc.Port
				break
			}
		}
	}
	opts := m.opts
	opts.FilesFrom = ""
	opts.FilesFromZero = false
	opts.ModifyWindow = 0
	args := opts.CopyArgs(port)
	destArg := dest.RsyncSpec()
	destArg = strings.TrimRight(destArg, "/") + "/"
	if dest.Kind == location.Local {
		destArg = strings.TrimRight(dest.Path, string(filepath.Separator)) + string(filepath.Separator)
	}
	args = append(args, "--")
	for _, raw := range m.sources {
		loc, err := location.Parse(raw)
		if err != nil {
			continue
		}
		args = append(args, location.ArgFor(loc, m.opts.ContentsOnly && !m.opts.GitTrackedOnly))
	}
	args = append(args, destArg)
	return config.JoinCommand(m.opts.Program, args)
}

func waitEvent(ch <-chan rsync.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return runClosedMsg{}
		}
		return runEventMsg{event: ev}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func detectRsyncVersion(program string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, program, "--version").Output()
	if err != nil {
		return "未找到 rsync"
	}
	line := strings.SplitN(string(out), "\n", 2)[0]
	re := regexp.MustCompile(`rsync\s+version\s+([0-9.]+)`)
	if m := re.FindStringSubmatch(line); m != nil {
		return "rsync " + m[1]
	}
	return strings.TrimSpace(line)
}

func isWSL() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl")
}

func sign(v int) int {
	if v < 0 {
		return -1
	}
	if v > 0 {
		return 1
	}
	return 0
}
