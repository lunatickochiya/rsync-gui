package rsync

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lunatickochiya/rsync-gui/tui/internal/config"
	"github.com/lunatickochiya/rsync-gui/tui/internal/location"
)

// Transfer is one rsync run: the local sources, or the sources of one remote host.
type Transfer struct {
	Origin          string
	Description     string
	Sources         []string
	LocalSourceDirs []string
	Remote          bool
	Port            int
}

// Plan is everything needed for a run.
type Plan struct {
	Transfers         []Transfer
	Destination       string // with a trailing slash, as passed to rsync
	DestinationRemote bool
	DestinationPort   int
	Options           config.Options
	TempFiles         []string
}

// Cleanup removes temporary files created for --files-from.
func (p *Plan) Cleanup() {
	for _, file := range p.TempFiles {
		_ = os.Remove(file)
	}
	p.TempFiles = nil
}

// BuildPlan validates the sources and destination and groups them into runs.
// Missing local destination directories are created.
func BuildPlan(opts config.Options, sourceStrings []string, destination string) (*Plan, error) {
	return buildPlan(opts, sourceStrings, destination, true)
}

// PreviewPlan is like BuildPlan but never creates the destination directory.
func PreviewPlan(opts config.Options, sourceStrings []string, destination string) (*Plan, error) {
	return buildPlan(opts, sourceStrings, destination, false)
}

func buildPlan(opts config.Options, sourceStrings []string, destination string, createDest bool) (*Plan, error) {
	opts.FilesFrom = ""
	opts.FilesFromZero = false
	opts.ModifyWindow = 0
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if len(sourceStrings) == 0 {
		return nil, fmt.Errorf("请先添加至少一个源")
	}
	if strings.TrimSpace(destination) == "" {
		return nil, fmt.Errorf("请选择目标目录")
	}

	dest, err := location.Parse(destination)
	if err != nil {
		return nil, fmt.Errorf("目标无效：%w", err)
	}
	if dest.Kind == location.Local {
		if info, err := os.Stat(dest.Path); err == nil && !info.IsDir() {
			return nil, fmt.Errorf("目标 %q 不是目录", location.DisplayPath(dest.Path))
		}
	}

	var sources []sourceItem
	var missing []string
	seen := make(map[string]bool)
	for _, raw := range sourceStrings {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		loc, err := location.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("源 %q 无效：%w", raw, err)
		}
		key := fmt.Sprintf("%s|%s|%d|%s", loc.Scheme, loc.Host, loc.Port, loc.Path)
		if seen[key] {
			continue
		}
		seen[key] = true
		isDir := false
		if loc.Kind == location.Local {
			info, err := os.Lstat(loc.Path)
			if err != nil {
				missing = append(missing, location.DisplayPath(loc.Path))
				continue
			}
			isDir = info.IsDir()
		}
		sources = append(sources, sourceItem{
			loc: loc, arg: location.ArgFor(loc, opts.ContentsOnly && !opts.GitTrackedOnly),
			isDir: isDir, rawKey: raw,
		})
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("以下源不存在：\n%s", strings.Join(missing, "\n"))
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("请先添加至少一个源")
	}

	// A destination inside a source directory would make rsync recurse into itself.
	if dest.Kind == location.Local {
		for _, src := range sources {
			if src.loc.Kind == location.Local && src.isDir && pathWithin(dest.Path, src.loc.Path) {
				return nil, fmt.Errorf("目标目录不能位于源目录 %q 内部", location.DisplayPath(src.loc.Path))
			}
		}
	}

	remoteSources := 0
	for _, src := range sources {
		if src.loc.Kind == location.Remote {
			remoteSources++
		}
	}
	if remoteSources > 0 && dest.Kind == location.Remote {
		return nil, fmt.Errorf("不支持在远程位置之间复制（请先下载到本地）")
	}

	plan := &Plan{
		DestinationRemote: dest.Kind == location.Remote,
		DestinationPort:   dest.Port,
	}
	if dest.Kind == location.Local {
		if !opts.DryRun && createDest {
			if err := os.MkdirAll(dest.Path, 0o755); err != nil {
				return nil, fmt.Errorf("无法创建目标目录 %q：%w", location.DisplayPath(dest.Path), err)
			}
		}
		plan.Destination = strings.TrimRight(dest.Path, string(filepath.Separator)) + string(filepath.Separator)
	} else {
		spec := dest.RsyncSpec()
		if strings.HasSuffix(spec, "/") {
			plan.Destination = spec
		} else {
			plan.Destination = spec + "/"
		}
	}

	opts = opts.Clone()
	if opts.GitTrackedOnly {
		filesFrom, root, err := prepareGitList(opts, sources, plan)
		if err != nil {
			plan.Cleanup()
			return nil, err
		}
		opts.FilesFrom = filesFrom
		opts.FilesFromZero = true
		// The list is relative to the repository root; pass the root without a slash.
		plan.Transfers = []Transfer{{
			Origin: "local", Description: "本机",
			Sources:         []string{root.Path},
			LocalSourceDirs: []string{root.Path},
		}}
	} else {
		byOrigin := make(map[string]int)
		for _, src := range sources {
			origin := "local"
			description := "本机"
			remote := false
			port := 0
			if src.loc.Kind == location.Remote {
				origin = fmt.Sprintf("%s|%s|%d", src.loc.User, src.loc.Host, src.loc.Port)
				description = src.loc.Host
				if src.loc.User != "" {
					description = src.loc.User + "@" + src.loc.Host
				}
				remote = true
				port = src.loc.Port
			}
			idx, ok := byOrigin[origin]
			if !ok {
				idx = len(plan.Transfers)
				byOrigin[origin] = idx
				plan.Transfers = append(plan.Transfers, Transfer{
					Origin: origin, Description: description, Remote: remote, Port: port,
				})
			}
			t := &plan.Transfers[idx]
			t.Sources = append(t.Sources, src.arg)
			if src.loc.Kind == location.Local && src.isDir {
				t.LocalSourceDirs = append(t.LocalSourceDirs, src.loc.Path)
			}
		}
	}

	if opts.AutoModifyWindow && !plan.DestinationRemote && (opts.Archive || opts.Times) {
		if fat, _ := isFATFileSystem(dest.Path, opts.DryRun); fat {
			opts.ModifyWindow = 1
		}
	}
	plan.Options = opts
	return plan, nil
}

func prepareGitList(opts config.Options, sources []sourceItem, plan *Plan) (string, location.Location, error) {
	if len(sources) != 1 || sources[0].loc.Kind != location.Local || !sources[0].isDir {
		return "", location.Location{}, fmt.Errorf("“仅 Git 跟踪文件”需要恰好一个本地仓库根目录作为源")
	}
	root := sources[0].loc
	out, err := exec.Command("git", "-C", root.Path, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", location.Location{}, fmt.Errorf("%q 不是 Git 工作树：%v", location.DisplayPath(root.Path), err)
	}
	top := strings.TrimSpace(string(out))
	topAbs, _ := filepath.Abs(top)
	rootAbs, _ := filepath.Abs(root.Path)
	if !samePath(topAbs, rootAbs) {
		return "", location.Location{}, fmt.Errorf("%q 不是 Git 工作树的顶层（顶层：%s）", location.DisplayPath(root.Path), location.DisplayPath(topAbs))
	}
	tracked, err := exec.Command("git", "-C", root.Path, "ls-files", "-z").Output()
	if err != nil {
		return "", location.Location{}, fmt.Errorf("git ls-files 失败：%v", err)
	}
	list := make([]byte, 0, len(tracked)+1024)
	list = append(list, tracked...)
	if opts.GitIncludeRepo {
		gitPath := filepath.Join(root.Path, ".git")
		info, statErr := os.Lstat(gitPath)
		if statErr != nil {
			return "", location.Location{}, fmt.Errorf("找不到 .git：%v", statErr)
		}
		if info.IsDir() {
			err := filepath.Walk(gitPath, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				rel, err := filepath.Rel(root.Path, path)
				if err != nil {
					return err
				}
				list = append(list, filepath.ToSlash(rel)...)
				list = append(list, 0)
				return nil
			})
			if err != nil {
				return "", location.Location{}, fmt.Errorf("无法列出 .git：%v", err)
			}
		} else {
			list = append(list, []byte(".git")...)
			list = append(list, 0)
		}
	}
	if len(bytes.Trim(list, "\x00")) == 0 {
		return "", location.Location{}, fmt.Errorf("%q 中没有 Git 跟踪的文件", location.DisplayPath(root.Path))
	}
	tmp, err := os.CreateTemp("", "rsync-tui-files-*")
	if err != nil {
		return "", location.Location{}, fmt.Errorf("无法创建文件列表：%v", err)
	}
	if _, err := tmp.Write(list); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", location.Location{}, fmt.Errorf("无法写入文件列表：%v", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", location.Location{}, fmt.Errorf("无法写入文件列表：%v", err)
	}
	plan.TempFiles = append(plan.TempFiles, tmp.Name())
	return tmp.Name(), root, nil
}

type sourceItem struct {
	loc    location.Location
	arg    string
	isDir  bool
	rawKey string
}

func pathWithin(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ai, errA := os.Stat(a)
	bi, errB := os.Stat(b)
	if errA == nil && errB == nil {
		return os.SameFile(ai, bi)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// CommandFor renders the full rsync command of a transfer for a phase.
func (p *Plan) CommandFor(t Transfer, phase Phase) []string {
	port := t.Port
	if p.DestinationRemote {
		port = p.DestinationPort
	}
	var args []string
	switch phase {
	case PhaseCopy:
		args = p.Options.CopyArgs(port)
	case PhaseVerify:
		args = p.Options.VerifyArgs(port)
	case PhaseRemove:
		args = p.Options.RemovalArgs(port)
	}
	args = append(args, "--")
	args = append(args, t.Sources...)
	args = append(args, p.Destination)
	return append([]string{p.Options.Program}, args...)
}
