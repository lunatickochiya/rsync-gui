package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/lunatickochiya/rsync-gui/tui/internal/config"
	"github.com/lunatickochiya/rsync-gui/tui/internal/location"
)

type rowKind int

const (
	rowHeader rowKind = iota
	rowBool
	rowChoice
	rowText
	rowAction
)

type optionRow struct {
	id     string
	kind   rowKind
	title  string
	flag   string
	indent bool
}

func optionRows() []optionRow {
	return []optionRow{
		{kind: rowHeader, title: "传输"},
		{id: "archive", kind: rowBool, title: "归档模式", flag: "-a"},
		{id: "recursive", kind: rowBool, title: "递归目录", flag: "-r", indent: true},
		{id: "links", kind: rowBool, title: "符号链接", flag: "-l", indent: true},
		{id: "perms", kind: rowBool, title: "权限", flag: "-p", indent: true},
		{id: "times", kind: rowBool, title: "修改时间", flag: "-t", indent: true},
		{id: "group", kind: rowBool, title: "所属组", flag: "-g", indent: true},
		{id: "owner", kind: rowBool, title: "所有者", flag: "-o", indent: true},
		{id: "devices", kind: rowBool, title: "设备与特殊文件", flag: "-D", indent: true},
		{id: "hardLinks", kind: rowBool, title: "硬链接", flag: "-H"},
		{id: "acls", kind: rowBool, title: "ACL", flag: "-A"},
		{id: "xattrs", kind: rowBool, title: "扩展属性", flag: "-X"},
		{id: "compress", kind: rowBool, title: "压缩传输", flag: "-z"},

		{kind: rowHeader, title: "进度与冲突"},
		{id: "progress", kind: rowChoice, title: "进度显示", flag: "--progress"},
		{id: "partial", kind: rowBool, title: "保留部分文件", flag: "--partial"},
		{id: "existing", kind: rowChoice, title: "已存在文件", flag: "-u"},
		{id: "deleteExtraneous", kind: rowBool, title: "删除目标多余文件", flag: "--delete"},
		{id: "dryRun", kind: rowBool, title: "试运行（不修改）", flag: "-n"},

		{kind: rowHeader, title: "校验与清理"},
		{id: "verifyAfter", kind: rowBool, title: "复制后校验", flag: "--checksum 比较"},
		{id: "checksum", kind: rowBool, title: "按校验和比较", flag: "-c"},
		{id: "removeSourceFiles", kind: rowBool, title: "删除源文件（移动）", flag: "--remove-source-files"},
		{id: "removeSourceDirs", kind: rowBool, title: "删除空的源目录", flag: "移动后", indent: true},

		{kind: rowHeader, title: "高级"},
		{id: "numericIds", kind: rowBool, title: "数字 ID", flag: "--numeric-ids"},
		{id: "oneFileSystem", kind: rowBool, title: "不跨文件系统", flag: "-x"},
		{id: "sparse", kind: rowBool, title: "稀疏文件", flag: "--sparse"},
		{id: "contentsOnly", kind: rowBool, title: "只复制目录内容", flag: "源目录加 /"},
		{id: "bwlimit", kind: rowText, title: "带宽限制", flag: "--bwlimit"},
		{id: "sshKey", kind: rowText, title: "SSH 密钥", flag: "-i"},
		{id: "excludeFile", kind: rowText, title: "排除文件", flag: "--exclude-from"},
		{id: "linkDest", kind: rowText, title: "硬链接基准", flag: "--link-dest"},
		{id: "remoteShell", kind: rowText, title: "远程 shell", flag: "-e"},
		{id: "gitTrackedOnly", kind: rowBool, title: "仅 Git 跟踪文件", flag: "--files-from"},
		{id: "gitIncludeRepo", kind: rowBool, title: "包含 .git", flag: "", indent: true},
		{id: "autoModifyWindow", kind: rowBool, title: "FAT 时间容差", flag: "--modify-window=1"},
		{id: "extraArguments", kind: rowText, title: "附加参数", flag: "任意 rsync 选项"},
		{id: "program", kind: rowText, title: "程序", flag: "rsync"},
		{id: "reset", kind: rowAction, title: "恢复默认选项", flag: ""},
	}
}

type rowState struct {
	checked  bool
	value    string
	disabled bool
	note     string
}

func (m *Model) optionState(id string) rowState {
	o := m.opts
	switch id {
	case "archive":
		return rowState{checked: o.Archive}
	case "recursive":
		return rowState{checked: o.Archive || o.Recursive, disabled: o.Archive}
	case "links":
		return rowState{checked: o.Archive || o.Links, disabled: o.Archive}
	case "perms":
		return rowState{checked: o.Archive || o.Perms, disabled: o.Archive}
	case "times":
		return rowState{checked: o.Archive || o.Times, disabled: o.Archive}
	case "group":
		return rowState{checked: o.Archive || o.Group, disabled: o.Archive}
	case "owner":
		return rowState{checked: o.Archive || o.Owner, disabled: o.Archive}
	case "devices":
		return rowState{checked: o.Archive || o.Devices, disabled: o.Archive}
	case "hardLinks":
		return rowState{checked: o.HardLinks}
	case "acls":
		return rowState{checked: o.ACLs}
	case "xattrs":
		return rowState{checked: o.Xattrs}
	case "compress":
		return rowState{checked: o.Compress}
	case "partial":
		return rowState{checked: o.Partial}
	case "checksum":
		return rowState{checked: o.Checksum}
	case "removeSourceFiles":
		return rowState{checked: o.RemoveSourceFiles}
	case "removeSourceDirs":
		return rowState{checked: o.RemoveSourceDirs, disabled: !o.RemoveSourceFiles}
	case "deleteExtraneous":
		return rowState{checked: o.DeleteExtraneous, note: conflictNote(o.DeleteExtraneous && o.RemoveSourceFiles)}
	case "dryRun":
		return rowState{checked: o.DryRun}
	case "verifyAfter":
		return rowState{checked: o.VerifyAfter, note: noteIf(o.VerifyAfter && o.DryRun, "试运行下跳过")}
	case "numericIds":
		return rowState{checked: o.NumericIDs}
	case "oneFileSystem":
		return rowState{checked: o.OneFileSystem}
	case "sparse":
		return rowState{checked: o.Sparse}
	case "contentsOnly":
		return rowState{checked: o.ContentsOnly, note: noteIf(o.ContentsOnly && o.GitTrackedOnly, "Git 模式下忽略")}
	case "gitTrackedOnly":
		return rowState{checked: o.GitTrackedOnly}
	case "gitIncludeRepo":
		return rowState{checked: o.GitIncludeRepo, disabled: !o.GitTrackedOnly}
	case "autoModifyWindow":
		return rowState{checked: o.AutoModifyWindow}
	case "progress":
		return rowState{value: progressLabel(o.Progress)}
	case "existing":
		return rowState{value: existingLabel(o.Existing)}
	case "bwlimit":
		return rowState{checked: o.UseBwLimit, value: o.BwLimit, note: noteIf(o.BwLimit != "" && !o.UseBwLimit, "已停用")}
	case "sshKey":
		return rowState{checked: o.SSHKey != "", value: o.SSHKey}
	case "excludeFile":
		return rowState{checked: o.ExcludeFile != "", value: o.ExcludeFile}
	case "linkDest":
		return rowState{checked: o.LinkDest != "", value: o.LinkDest}
	case "remoteShell":
		return rowState{checked: o.UseRemoteShell, value: o.RemoteShell, note: noteIf(o.RemoteShell != "" && !o.UseRemoteShell, "已停用")}
	case "extraArguments":
		return rowState{checked: o.ExtraArguments != "", value: o.ExtraArguments}
	case "program":
		return rowState{checked: o.Program != "", value: o.Program}
	}
	return rowState{}
}

func conflictNote(show bool) string {
	if show {
		return "与移动冲突"
	}
	return ""
}

func noteIf(cond bool, text string) string {
	if cond {
		return text
	}
	return ""
}

func progressLabel(value string) string {
	switch value {
	case "overall":
		return "总体"
	case "none":
		return "关闭"
	default:
		return "每文件"
	}
}

func existingLabel(value string) string {
	switch value {
	case "skip-newer":
		return "跳过较新"
	case "ignore-existing":
		return "忽略已存在"
	default:
		return "覆盖"
	}
}

func (m *Model) toggleCurrentOption(edit bool) {
	rows := optionRows()
	if m.optionCursor < 0 || m.optionCursor >= len(rows) {
		return
	}
	row := rows[m.optionCursor]
	if row.kind == rowHeader {
		return
	}
	if row.kind == rowAction {
		m.modal = modalState{kind: modalConfirm, title: "恢复默认选项？", body: "当前的 rsync 选项将回到默认值（源和目标保持不变）。", action: "reset-options", confirmLabel: "恢复默认"}
		return
	}
	if row.kind == rowText {
		state := m.optionState(row.id)
		if !edit && state.value != "" && (row.id == "bwlimit" || row.id == "remoteShell") {
			if row.id == "bwlimit" {
				m.opts.UseBwLimit = !m.opts.UseBwLimit
			} else {
				m.opts.UseRemoteShell = !m.opts.UseRemoteShell
			}
			m.markCustom()
			return
		}
		m.openOptionEditor(row.id)
		return
	}
	state := m.optionState(row.id)
	if state.disabled {
		return
	}
	o := &m.opts
	if row.kind == rowChoice {
		switch row.id {
		case "progress":
			switch o.Progress {
			case "per-file":
				o.Progress = "overall"
			case "overall":
				o.Progress = "none"
			default:
				o.Progress = "per-file"
			}
		case "existing":
			switch o.Existing {
			case "replace":
				o.Existing = "skip-newer"
			case "skip-newer":
				o.Existing = "ignore-existing"
			default:
				o.Existing = "replace"
			}
		}
		m.markCustom()
		return
	}
	switch row.id {
	case "archive":
		o.Archive = !o.Archive
	case "recursive":
		o.Recursive = !o.Recursive
	case "links":
		o.Links = !o.Links
	case "perms":
		o.Perms = !o.Perms
	case "times":
		o.Times = !o.Times
	case "group":
		o.Group = !o.Group
	case "owner":
		o.Owner = !o.Owner
	case "devices":
		o.Devices = !o.Devices
	case "hardLinks":
		o.HardLinks = !o.HardLinks
	case "acls":
		o.ACLs = !o.ACLs
	case "xattrs":
		o.Xattrs = !o.Xattrs
	case "compress":
		o.Compress = !o.Compress
	case "partial":
		o.Partial = !o.Partial
	case "checksum":
		o.Checksum = !o.Checksum
	case "removeSourceFiles":
		o.RemoveSourceFiles = !o.RemoveSourceFiles
		if o.RemoveSourceFiles && o.DeleteExtraneous {
			o.DeleteExtraneous = false
			m.setToast("已关闭 --delete：不能与删除源文件同时使用", true)
		}
		if !o.RemoveSourceFiles {
			o.RemoveSourceDirs = false
		}
	case "removeSourceDirs":
		o.RemoveSourceDirs = !o.RemoveSourceDirs
	case "deleteExtraneous":
		o.DeleteExtraneous = !o.DeleteExtraneous
		if o.DeleteExtraneous && o.RemoveSourceFiles {
			o.RemoveSourceFiles = false
			o.RemoveSourceDirs = false
			m.setToast("已关闭删除源文件：不能与 --delete 同时使用", true)
		}
	case "dryRun":
		o.DryRun = !o.DryRun
	case "verifyAfter":
		o.VerifyAfter = !o.VerifyAfter
	case "numericIds":
		o.NumericIDs = !o.NumericIDs
	case "oneFileSystem":
		o.OneFileSystem = !o.OneFileSystem
	case "sparse":
		o.Sparse = !o.Sparse
	case "contentsOnly":
		o.ContentsOnly = !o.ContentsOnly
	case "gitTrackedOnly":
		o.GitTrackedOnly = !o.GitTrackedOnly
		if o.GitTrackedOnly && o.RemoveSourceFiles {
			o.RemoveSourceFiles = false
			o.RemoveSourceDirs = false
			m.setToast("Git 跟踪文件模式不支持移动", true)
		}
	case "gitIncludeRepo":
		o.GitIncludeRepo = !o.GitIncludeRepo
	case "autoModifyWindow":
		o.AutoModifyWindow = !o.AutoModifyWindow
	}
	m.markCustom()
}

func (m *Model) openOptionEditor(id string) {
	switch id {
	case "bwlimit":
		m.openInput("bwlimit", "带宽限制", "例如 500、1.5M、10m；留空即停用", m.opts.BwLimit)
	case "sshKey":
		m.openInput("sshKey", "SSH 私钥", "例如 ~/.ssh/id_ed25519；用于远程源/目标", m.opts.SSHKey)
	case "excludeFile":
		m.openInput("excludeFile", "排除文件", "每行一个 rsync 排除模式（--exclude-from）", m.opts.ExcludeFile)
	case "linkDest":
		m.openInput("linkDest", "硬链接基准", "例如上一次快照目录（--link-dest）", m.opts.LinkDest)
	case "remoteShell":
		m.openInput("remoteShell", "远程 shell", "例如 ssh -o StrictHostKeyChecking=accept-new；留空即停用", m.opts.RemoteShell)
	case "extraArguments":
		m.openInput("extraArguments", "附加参数", "例如 --exclude='*.tmp' --mkpath", m.opts.ExtraArguments)
	case "program":
		m.openInput("program", "rsync 程序", "可执行文件名或绝对路径", m.opts.Program)
	}
}

func (m *Model) applyPreset(p config.Preset) {
	if p.Error != "" {
		m.setToast("预设无效："+p.Error, true)
		return
	}
	m.opts = config.MergePreset(m.opts, p.Options)
	m.activePreset = p.ID
	m.modal = modalState{}
	m.setToast("已载入预设："+p.Label, false)
}

func (m *Model) commitInput(value string) (bool, string) {
	value = strings.TrimSpace(value)
	field := m.modal.field
	switch field {
	case "source", "source-edit":
		if value == "" {
			return false, "路径不能为空"
		}
		loc, err := location.Parse(value)
		if err != nil {
			return false, err.Error()
		}
		arg := loc.StoreSpec()
		if field == "source-edit" {
			if m.sourceCursor >= 0 && m.sourceCursor < len(m.sources) {
				m.sources[m.sourceCursor] = arg
			}
			m.setToast("已更新源", false)
		} else {
			for _, existing := range m.sources {
				if existing == arg {
					m.setToast("该源已在列表中", true)
					m.modal = modalState{}
					return true, ""
				}
			}
			m.sources = append(m.sources, arg)
			m.sourceCursor = len(m.sources) - 1
			if loc.IsRemote() {
				m.setToast("已添加远程源 "+arg, false)
			} else if _, statErr := os.Stat(loc.Path); statErr != nil {
				m.setToast("已添加，但路径不存在（开始时会报错）", true)
			} else {
				m.setToast("已添加源", false)
			}
		}
		m.focus = focusSources
		m.modal = modalState{}
		return true, ""
	case "dest":
		if value == "" {
			return false, "目标不能为空"
		}
		loc, err := location.Parse(value)
		if err != nil {
			return false, err.Error()
		}
		m.dest = loc.StoreSpec()
		m.cfg.RememberDestination(m.dest)
		m.setToast("目标已设置："+loc.Display(), false)
		m.modal = modalState{}
		return true, ""
	case "bwlimit":
		m.opts.BwLimit = value
		m.opts.UseBwLimit = value != ""
		m.markCustom()
		m.modal = modalState{}
		return true, ""
	case "remoteShell":
		m.opts.RemoteShell = value
		m.opts.UseRemoteShell = value != ""
		m.markCustom()
		m.modal = modalState{}
		return true, ""
	case "sshKey":
		m.opts.SSHKey = value
		m.markCustom()
		m.modal = modalState{}
		return true, ""
	case "excludeFile":
		m.opts.ExcludeFile = value
		m.markCustom()
		m.modal = modalState{}
		return true, ""
	case "linkDest":
		m.opts.LinkDest = value
		m.markCustom()
		m.modal = modalState{}
		return true, ""
	case "extraArguments":
		if _, err := config.SplitArgs(value); err != nil {
			return false, "引号未闭合"
		}
		m.opts.ExtraArguments = value
		m.markCustom()
		m.modal = modalState{}
		return true, ""
	case "program":
		if value == "" {
			return false, "程序不能为空"
		}
		m.opts.Program = value
		m.rsyncVersion = detectRsyncVersion(value)
		m.markCustom()
		m.modal = modalState{}
		return true, ""
	case "import-settings":
		if value == "" {
			return false, "请填写设置文件路径"
		}
		loaded, _, err := config.LoadSettingsFile(expandUser(value), m.opts)
		if err != nil {
			return false, err.Error()
		}
		m.opts = loaded
		m.activePreset = ""
		m.rsyncVersion = detectRsyncVersion(m.opts.Program)
		m.modal = modalState{}
		m.setToast("已导入设置："+expandUser(value), false)
		return true, ""
	case "export-settings":
		if value == "" {
			return false, "请填写保存路径"
		}
		path := expandUser(value)
		if err := config.SaveSettingsFile(path, "", m.opts); err != nil {
			return false, err.Error()
		}
		m.modal = modalState{}
		m.setToast("已导出设置："+path, false)
		return true, ""
	}
	m.modal = modalState{}
	return true, ""
}

func (m *Model) commitBulk() {
	lines := strings.Split(m.bulk.Value(), "\n")
	added := 0
	seen := make(map[string]bool, len(m.sources))
	for _, s := range m.sources {
		seen[s] = true
	}
	var invalid []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		loc, err := location.Parse(line)
		if err != nil {
			invalid = append(invalid, line)
			continue
		}
		arg := loc.StoreSpec()
		if seen[arg] {
			continue
		}
		seen[arg] = true
		m.sources = append(m.sources, arg)
		added++
	}
	m.bulk.Blur()
	m.modal = modalState{}
	m.clampSourceCursor()
	if added > 0 {
		m.sourceCursor = len(m.sources) - 1
		m.setToast(fmt.Sprintf("已添加 %d 个源", added), false)
	} else if len(invalid) > 0 {
		m.setToast("没有添加：路径无效", true)
	} else {
		m.setToast("没有新的源", true)
	}
}

func (m *Model) resetOptions() {
	m.opts = config.DefaultOptions()
	m.activePreset = ""
	m.optionCursor = 1
	m.optionScroll = 0
	m.modal = modalState{}
	m.setToast("已恢复默认选项", false)
}

func findPreset(items []config.Preset, id string) (config.Preset, bool) {
	for _, p := range items {
		if p.ID == id {
			return p, true
		}
	}
	return config.Preset{}, false
}
