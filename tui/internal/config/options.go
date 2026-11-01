package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Options is the supported subset of rsync options. It mirrors the settings file
// format of rsync-gui, so presets and exported settings can be shared.
type Options struct {
	Program string `json:"program"`

	Archive   bool `json:"archive"`
	Recursive bool `json:"recursive"`
	Links     bool `json:"links"`
	Perms     bool `json:"perms"`
	Times     bool `json:"times"`
	Group     bool `json:"group"`
	Owner     bool `json:"owner"`
	Devices   bool `json:"devices"`

	HardLinks bool `json:"hardLinks"`
	ACLs      bool `json:"acls"`
	Xattrs    bool `json:"xattrs"`

	Verbose bool `json:"verbose"`
	Human   bool `json:"human"`
	Partial bool `json:"partial"`

	Progress string `json:"progress"` // none, per-file, overall
	Checksum bool   `json:"checksum"`

	NumericIDs     bool `json:"numericIds"`
	OneFileSystem  bool `json:"oneFileSystem"`
	Sparse         bool `json:"sparse"`
	GitTrackedOnly bool `json:"gitTrackedOnly"`
	GitIncludeRepo bool `json:"gitIncludeRepo"`

	Existing string `json:"existing"` // replace, skip-newer, ignore-existing

	Compress          bool `json:"compress"`
	ContentsOnly      bool `json:"contentsOnly"`
	DeleteExtraneous  bool `json:"deleteExtraneous"`
	RemoveSourceFiles bool `json:"removeSourceFiles"`
	RemoveSourceDirs  bool `json:"removeSourceDirs"`
	DryRun            bool `json:"dryRun"`
	VerifyAfter       bool `json:"verifyAfter"`

	UseBwLimit     bool   `json:"useBwLimit"`
	BwLimit        string `json:"bwLimit"`
	UseRemoteShell bool   `json:"useRemoteShell"`
	RemoteShell    string `json:"remoteShell"`
	SSHKey         string `json:"sshKey"`
	ExcludeFile    string `json:"excludeFile"`
	LinkDest       string `json:"linkDest"`

	ExtraArguments   string `json:"extraArguments"`
	AutoModifyWindow bool   `json:"autoModifyWindow"`

	// Runtime-only values; they are not written to settings files.
	FilesFrom     string `json:"-"`
	FilesFromZero bool   `json:"-"`
	ModifyWindow  int    `json:"-"`
}

const SettingsFormat = "rsync-gui-settings"
const SettingsVersion = 1

// DefaultOptions matches the defaults of the Qt application.
func DefaultOptions() Options {
	return Options{
		Program:          "rsync",
		Archive:          true,
		Recursive:        true,
		Links:            true,
		Perms:            true,
		Times:            true,
		Group:            true,
		Owner:            true,
		Devices:          true,
		Verbose:          true,
		Human:            true,
		Partial:          true,
		Progress:         "per-file",
		GitIncludeRepo:   true,
		Existing:         "replace",
		AutoModifyWindow: true,
	}
}

func (o Options) Clone() Options { return o }

// CanVerify reports whether a verification pass makes sense.
func (o Options) CanVerify() bool { return o.VerifyAfter && !o.DryRun }

// VerifiedMove is a move whose sources are removed only after verification.
func (o Options) VerifiedMove() bool { return o.RemoveSourceFiles && o.CanVerify() }

// RemovesSourceDirs reports whether emptied source directories are cleaned up.
func (o Options) RemovesSourceDirs() bool {
	return o.RemoveSourceFiles && o.RemoveSourceDirs && !o.DryRun
}

// CopyArgs returns the arguments of the copy pass.
func (o Options) CopyArgs(port int) []string {
	return o.buildArgs(port, o.RemoveSourceFiles && !o.VerifiedMove())
}

// RemovalArgs returns the arguments of the source-removal pass of a verified move.
func (o Options) RemovalArgs(port int) []string { return o.buildArgs(port, true) }

// VerifyArgs returns the arguments of the checksum dry-run verification pass.
func (o Options) VerifyArgs(port int) []string {
	flags := "n"
	if o.Archive || o.Recursive {
		flags += "r"
	}
	if o.Archive || o.Links {
		flags += "l"
	}
	if o.OneFileSystem {
		flags += "x"
	}
	flags += "ci"

	args := []string{"-" + flags, "--out-format=%i %n%L"}
	if o.NumericIDs {
		args = append(args, "--numeric-ids")
	}
	if o.FilesFrom != "" {
		args = append(args, "--files-from="+o.FilesFrom)
		if o.FilesFromZero {
			args = append(args, "--from0")
		}
	}
	if o.ExcludeFile != "" {
		args = append(args, "--exclude-from="+expandHome(o.ExcludeFile))
	}
	args = append(args, o.remoteShellArgs(port)...)
	extra, _ := SplitArgs(o.ExtraArguments)
	args = append(args, extra...)
	return args
}

func (o Options) buildArgs(port int, removeSources bool) []string {
	var flags strings.Builder
	var long []string

	if o.Archive {
		flags.WriteByte('a')
	} else {
		for _, part := range []struct {
			on   bool
			flag byte
		}{
			{o.Recursive, 'r'}, {o.Links, 'l'}, {o.Perms, 'p'}, {o.Times, 't'},
			{o.Group, 'g'}, {o.Owner, 'o'}, {o.Devices, 'D'},
		} {
			if part.on {
				flags.WriteByte(part.flag)
			}
		}
	}
	if o.HardLinks {
		flags.WriteByte('H')
	}
	if o.ACLs {
		flags.WriteByte('A')
	}
	if o.Xattrs {
		flags.WriteByte('X')
	}
	if o.OneFileSystem {
		flags.WriteByte('x')
	}
	if o.Verbose {
		flags.WriteByte('v')
	}
	if o.Human {
		flags.WriteByte('h')
	}
	switch o.Progress {
	case "none":
		if o.Partial {
			long = append(long, "--partial")
		}
	case "per-file":
		if o.Partial {
			flags.WriteByte('P') // -P = --partial --progress
		} else {
			long = append(long, "--progress")
		}
	case "overall":
		if o.Partial {
			long = append(long, "--partial")
		}
		long = append(long, "--info=progress2")
	}
	if o.Checksum {
		flags.WriteByte('c')
	}
	if o.Existing == "skip-newer" {
		flags.WriteByte('u')
	}
	if o.Compress {
		flags.WriteByte('z')
	}
	if o.DryRun {
		flags.WriteByte('n')
	}
	if o.Existing == "ignore-existing" {
		long = append(long, "--ignore-existing")
	}
	if o.DeleteExtraneous {
		long = append(long, "--delete")
	}
	if removeSources {
		long = append(long, "--remove-source-files")
	}
	if o.UseBwLimit {
		long = append(long, "--bwlimit="+strings.TrimSpace(o.BwLimit))
	}
	if o.NumericIDs {
		long = append(long, "--numeric-ids")
	}
	if o.Sparse {
		long = append(long, "--sparse")
	}
	if o.LinkDest != "" {
		long = append(long, "--link-dest="+expandHome(o.LinkDest))
	}
	if o.FilesFrom != "" {
		long = append(long, "--files-from="+o.FilesFrom)
		if o.FilesFromZero {
			long = append(long, "--from0")
		}
	}
	if o.ExcludeFile != "" {
		long = append(long, "--exclude-from="+expandHome(o.ExcludeFile))
	}
	extra, _ := SplitArgs(o.ExtraArguments)
	if o.ModifyWindow > 0 && (o.Archive || o.Times) && !hasModifyWindow(extra) {
		long = append(long, "--modify-window="+strconv.Itoa(o.ModifyWindow))
	}

	args := make([]string, 0, 8+len(long)+len(extra))
	if flags.Len() > 0 {
		args = append(args, "-"+flags.String())
	}
	args = append(args, long...)
	args = append(args, o.remoteShellArgs(port)...)
	args = append(args, extra...)
	return args
}

func hasModifyWindow(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "--modify-window") || strings.HasPrefix(arg, "-@") {
			return true
		}
	}
	return false
}

func (o Options) remoteShellArgs(port int) []string {
	shell := ""
	if o.UseRemoteShell {
		shell = strings.TrimSpace(o.RemoteShell)
	}
	if key := strings.TrimSpace(o.SSHKey); key != "" {
		if shell == "" {
			shell = "ssh"
		}
		shell += " -i " + ShellQuote(expandHome(key))
	}
	if port > 0 {
		if shell == "" {
			shell = "ssh"
		}
		shell += " -p " + strconv.Itoa(port)
	}
	if shell == "" {
		return nil
	}
	return []string{"-e", shell}
}

// SummaryFlags returns the flags that decide what is copied, for preset labels.
func (o Options) SummaryFlags() string {
	var flags strings.Builder
	if o.Archive {
		flags.WriteByte('a')
	} else {
		for _, part := range []struct {
			on   bool
			flag byte
		}{
			{o.Recursive, 'r'}, {o.Links, 'l'}, {o.Perms, 'p'}, {o.Times, 't'},
			{o.Group, 'g'}, {o.Owner, 'o'}, {o.Devices, 'D'},
		} {
			if part.on {
				flags.WriteByte(part.flag)
			}
		}
	}
	for _, part := range []struct {
		on   bool
		flag byte
	}{
		{o.HardLinks, 'H'}, {o.ACLs, 'A'}, {o.Xattrs, 'X'}, {o.Checksum, 'c'},
		{o.OneFileSystem, 'x'}, {o.Existing == "skip-newer", 'u'}, {o.Compress, 'z'},
	} {
		if part.on {
			flags.WriteByte(part.flag)
		}
	}
	var parts []string
	if flags.Len() > 0 {
		parts = append(parts, "-"+flags.String())
	}
	if o.Existing == "ignore-existing" {
		parts = append(parts, "--ignore-existing")
	}
	if o.DeleteExtraneous {
		parts = append(parts, "--delete")
	}
	return strings.Join(parts, " ")
}

// Label renders "name (flags)" like the Qt application.
func (o Options) Label(name string) string {
	if flags := o.SummaryFlags(); flags != "" {
		return fmt.Sprintf("%s (%s)", name, flags)
	}
	return name
}

// ModeName describes the high-level operation.
func (o Options) ModeName() string {
	if o.RemoveSourceFiles {
		return "移动"
	}
	return "复制"
}

// Validate checks the options before a run.
func (o Options) Validate() error {
	if strings.TrimSpace(o.Program) == "" {
		return errors.New("未指定 rsync 程序")
	}
	if strings.Contains(o.Program, "/") {
		info, err := os.Stat(o.Program)
		if err != nil || info.IsDir() || info.Mode()&0111 == 0 {
			return fmt.Errorf("程序 %q 不存在或不可执行", o.Program)
		}
	} else if _, err := exec.LookPath(o.Program); err != nil {
		return fmt.Errorf("程序 %q 不存在或不可执行", o.Program)
	}
	if o.UseBwLimit {
		limit := strings.TrimSpace(o.BwLimit)
		ok, _ := regexp.MatchString(`^[0-9]+(\.[0-9]+)?([bBkKmMgGtTpP](i?[bB])?)?$`, limit)
		if !ok {
			return fmt.Errorf("带宽限制 %q 无效（例如 500、1.5M、10m、100K）", o.BwLimit)
		}
	}
	if o.UseRemoteShell && strings.TrimSpace(o.RemoteShell) == "" {
		return errors.New("远程 shell 命令为空")
	}
	if key := strings.TrimSpace(o.SSHKey); key != "" {
		info, err := os.Stat(expandHome(key))
		if err != nil || info.IsDir() {
			return fmt.Errorf("SSH 密钥 %q 不存在或不可读", o.SSHKey)
		}
	}
	if o.ExcludeFile != "" {
		info, err := os.Stat(expandHome(o.ExcludeFile))
		if err != nil || info.IsDir() {
			return fmt.Errorf("排除文件 %q 不存在或不可读", o.ExcludeFile)
		}
	}
	if _, err := SplitArgs(o.ExtraArguments); err != nil {
		return fmt.Errorf("附加参数：引号未闭合")
	}
	if o.DeleteExtraneous && o.RemoveSourceFiles {
		return errors.New("--delete 与 --remove-source-files 不能同时使用")
	}
	if o.GitTrackedOnly && o.RemoveSourceFiles {
		return errors.New("仅 Git 跟踪文件模式不支持移动（源文件列表不完整）")
	}
	return nil
}

func expandHome(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// SettingsFile is the on-disk JSON envelope used by rsync-gui.
type SettingsFile struct {
	Format  string          `json:"format"`
	Version int             `json:"version"`
	Name    string          `json:"name,omitempty"`
	Options json.RawMessage `json:"options"`
}

// LoadSettingsFile reads a preset or settings file. Missing option keys keep the
// values from base.
func LoadSettingsFile(path string, base Options) (Options, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return base, "", fmt.Errorf("无法读取 %q：%w", path, err)
	}
	var root SettingsFile
	if err := json.Unmarshal(data, &root); err != nil {
		return base, "", fmt.Errorf("%q 不是有效的 JSON：%w", path, err)
	}
	if root.Format != SettingsFormat {
		return base, "", fmt.Errorf("%q 不是 rsync 设置文件", path)
	}
	if root.Version < 1 || root.Version > SettingsVersion {
		return base, "", fmt.Errorf("不支持设置文件版本 %d（支持：%d）", root.Version, SettingsVersion)
	}
	if len(root.Options) == 0 {
		return base, "", fmt.Errorf("%q 缺少 options 对象", path)
	}
	opts := base
	if err := json.Unmarshal(root.Options, &opts); err != nil {
		return base, "", fmt.Errorf("%q 中的选项无效：%w", path, err)
	}
	if opts.Progress == "" {
		opts.Progress = base.Progress
	}
	if opts.Existing == "" {
		opts.Existing = base.Existing
	}
	if opts.Progress != "none" && opts.Progress != "per-file" && opts.Progress != "overall" {
		return base, "", fmt.Errorf("%q：progress 必须是 none、per-file 或 overall", path)
	}
	if opts.Existing != "replace" && opts.Existing != "skip-newer" && opts.Existing != "ignore-existing" {
		return base, "", fmt.Errorf("%q：existing 必须是 replace、skip-newer 或 ignore-existing", path)
	}
	return opts, root.Name, nil
}

// SaveSettingsFile writes options in the rsync-gui settings format.
func SaveSettingsFile(path, name string, opts Options) error {
	raw, err := json.Marshal(opts)
	if err != nil {
		return err
	}
	root := SettingsFile{Format: SettingsFormat, Version: SettingsVersion, Name: name, Options: raw}
	data, err := json.MarshalIndent(root, "", "    ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
