package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) renderModal() string {
	maxW := min(88, m.width-4)
	if maxW < 30 {
		maxW = m.width - 2
	}
	innerW := maxW - 6
	if innerW < 20 {
		innerW = 20
	}
	var content string
	hintText := m.modal.hint
	switch m.modal.kind {
	case modalInput:
		content, hintText = m.inputModal(innerW)
	case modalBulk:
		content, hintText = m.bulkModal(innerW)
	case modalPresets:
		content, hintText = m.presetModal(innerW)
	case modalConfirm:
		content, hintText = m.confirmModal(innerW)
	case modalHelp:
		content, hintText = m.helpModal(innerW)
	case modalMessage:
		content, hintText = m.messageModal(innerW)
	case modalCommand:
		content, hintText = m.commandModal(innerW)
	}
	if hintText != "" {
		content += "\n\n" + styFaint.Render(hintText)
	}
	box := lipgloss.NewStyle().
		Background(cPanel).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cBorder).
		Padding(1, 2).
		Width(maxW - 6).
		Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceBackground(cBg))
}

func (m *Model) modalTitle(title, subtitle string) string {
	out := styAccentBold.Render(title)
	if subtitle != "" {
		out += "\n" + styMuted.Render(subtitle)
	}
	return out
}

func (m *Model) inputModal(width int) (string, string) {
	title := m.modalTitle(m.modal.title, m.modal.subtitle)
	m.input.Width = max(20, width-4)
	body := title + "\n\n" + m.input.View()
	if m.modal.err != "" {
		body += "\n" + styRed.Render("! "+m.modal.err)
	}
	if len(m.completions) > 0 {
		body += "\n\n" + styFaint.Render("补全候选")
		maxShow := 6
		if m.height < 22 {
			maxShow = 2
		} else if m.height < 27 {
			maxShow = 4
		}
		maxShow = min(maxShow, len(m.completions))
		for i := 0; i < maxShow; i++ {
			line := truncateMiddle(m.completions[i], width-2)
			if i == m.completionIdx {
				line = styAccent.Render(line)
			} else {
				line = styMuted.Render(line)
			}
			body += "\n  " + line
		}
		if len(m.completions) > maxShow {
			body += "\n  " + styFaint.Render(fmt.Sprintf("… 还有 %d 项", len(m.completions)-maxShow))
		}
	}
	if m.modal.field == "dest" {
		recents := m.recentCandidates()
		if len(recents) > 0 {
			body += "\n\n" + styFaint.Render("最近目标")
			maxRecent := 4
			if m.height < 22 {
				maxRecent = 0
			} else if m.height < 27 {
				maxRecent = 2
			}
			if maxRecent == 0 {
				return body, "tab 补全 · enter 保存 · esc 取消"
			}
			maxShow := min(maxRecent, len(recents))
			for i := 0; i < maxShow; i++ {
				line := truncateMiddle(recents[i], width-2)
				if i == m.recentIdx {
					line = styAccent.Render(line)
				} else {
					line = styMuted.Render(line)
				}
				body += "\n  " + line
			}
		}
	}
	hintText := "enter 保存 · esc 取消"
	switch m.modal.field {
	case "source", "source-edit":
		hintText = "tab 补全 · enter 添加 · esc 取消"
	case "dest":
		hintText = "tab 补全 · ctrl+n 最近目标 · enter 保存 · esc 取消"
	case "import-settings", "export-settings":
		hintText = "tab 补全 · enter 保存 · esc 取消"
	}
	return body, hintText
}

func (m *Model) bulkModal(width int) (string, string) {
	body := m.modalTitle(m.modal.title, "可以一次粘贴多行；空行和 # 开头的行会被忽略")
	m.bulk.SetWidth(max(24, width-4))
	m.bulk.SetHeight(clamp(m.height-14, 4, 14))
	body += "\n\n" + m.bulk.View()
	return body, "ctrl+s 添加 · esc 取消"
}

func (m *Model) presetModal(width int) (string, string) {
	items := m.modal.items
	body := m.modalTitle(m.modal.title, fmt.Sprintf("%d 个可用预设；用户预设优先于内置预设", len(items)))
	body += "\n"
	visible := max(3, (m.height-12)/2)
	start := 0
	if m.modal.cursor >= visible {
		start = m.modal.cursor - visible + 1
	}
	end := min(len(items), start+visible)
	for i := start; i < end; i++ {
		p := items[i]
		selected := i == m.modal.cursor
		bar := "  "
		if selected {
			bar = styAccent.Render("▌") + " "
		}
		label := truncate(p.Label, width-12)
		labelStyle := styText
		if selected {
			labelStyle = styAccentBold
		}
		tag := "内置"
		if !p.Builtin {
			tag = "用户"
		}
		right := styFaint.Render(tag) + " "
		gap := max(1, width-lipgloss.Width(bar)-lipgloss.Width(label)-lipgloss.Width(right)-4)
		body += "\n" + bar + labelStyle.Render(label) + strings.Repeat(" ", gap) + right
		detail := p.ID
		if p.Error != "" {
			detail = "错误：" + p.Error
		} else if p.Path != "" {
			detail = p.Path
		}
		detailStyle := styFaint
		if p.Error != "" {
			detailStyle = styRed
		}
		body += "\n    " + detailStyle.Render(truncateMiddle(detail, width-6))
		if i != end-1 {
			body += "\n"
		}
	}
	if len(items) == 0 {
		body += "\n" + styMuted.Render("没有预设")
	}
	return body, "↑↓ 选择 · enter 应用 · esc 取消"
}

func (m *Model) confirmModal(width int) (string, string) {
	body := m.modalTitle(m.modal.title, "")
	if m.modal.body != "" {
		body += "\n\n" + strings.Join(wrapText(m.modal.body, width-2), "\n")
	}
	body += "\n\n"
	label := m.modal.confirmLabel
	if label == "" {
		label = "确定"
	}
	body += "  " + styMuted.Render("[ esc 取消 ]") + "     " +
		lipgloss.NewStyle().Foreground(cBg).Background(cAccent).Bold(true).Padding(0, 1).Render(label)
	return body, "enter 确认 · esc 返回"
}

func (m *Model) helpModal(width int) (string, string) {
	lines := helpLines()
	visible := max(4, m.height-14)
	m.modal.scroll = clamp(m.modal.scroll, 0, max(0, len(lines)-visible))
	end := min(len(lines), m.modal.scroll+visible)
	body := m.modalTitle(m.modal.title, "全部操作都可以只用键盘完成")
	body += "\n\n" + strings.Join(lines[m.modal.scroll:end], "\n")
	return body, "↑↓ 滚动 · esc 关闭"
}

func helpLines() []string {
	var out []string
	section := func(s string) {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, styMuted.Bold(true).Render(s))
	}
	item := func(key, text string) {
		out = append(out, "  "+lipgloss.NewStyle().Foreground(cAccent).Width(11).Render(key)+styText.Render(text))
	}
	section("全局")
	item("tab", "在“源”和“选项”之间切换")
	item("s / o", "直接跳到源 / 选项")
	item("↑↓ j k", "移动光标；pgup/pgdown 翻页")
	item("?", "打开这个帮助")
	item("q", "退出（运行中请先取消）")
	section("源与目标")
	item("a", "添加一个源（支持远程路径）")
	item("A", "批量粘贴多个源")
	item("e", "编辑选中的源")
	item("x", "移除选中的源")
	item("c", "清空源列表")
	item("d", "编辑目标目录；输入时 ctrl+n 选最近目标")
	item("tab", "输入路径时补全文件和目录")
	section("选项")
	item("space", "开关选项；choice 类型循环取值")
	item("enter", "编辑文本选项 / 开关选项")
	item("p", "载入预设（复制、校验、移动、FAT/NAS…）")
	item("t", "快速开关试运行")
	item("v", "查看完整 rsync 命令")
	item("I / E", "导入 / 导出 JSON 设置")
	section("运行")
	item("r", "开始传输；会删除文件时先确认")
	item("esc", "运行中取消（再确认一次）")
	item("v", "查看本次运行的完整命令")
	item("↑↓", "滚动日志；end 回到跟随")
	item("enter", "结束后返回设置")
	section("说明")
	out = append(out, "  "+styMuted.Render("远程传输请使用 SSH 密钥或 ssh-agent；TUI 不处理密码提示。"))
	out = append(out, "  "+styMuted.Render("源和选项会自动保存到 ~/.config/rsync-tui/config.json。"))
	return out
}

func (m *Model) messageModal(width int) (string, string) {
	body := m.modalTitle(m.modal.title, "")
	wrapped := wrapText(m.modal.body, width-2)
	visible := max(4, m.height-12)
	m.modal.scroll = clamp(m.modal.scroll, 0, max(0, len(wrapped)-visible))
	end := min(len(wrapped), m.modal.scroll+visible)
	body += "\n\n" + strings.Join(wrapped[m.modal.scroll:end], "\n")
	if len(wrapped) > visible {
		body += "\n" + styFaint.Render(fmt.Sprintf("↑↓ 滚动（%d/%d 行）", end, len(wrapped)))
	}
	return body, "enter 关闭"
}

func (m *Model) commandModal(width int) (string, string) {
	body := m.modalTitle(m.modal.title, "按阶段实际执行的命令会写入运行日志")
	cmd := m.modal.command
	lines := wrapText(cmd, width-2)
	visible := max(4, m.height-14)
	m.modal.scroll = clamp(m.modal.scroll, 0, max(0, len(lines)-visible))
	end := min(len(lines), m.modal.scroll+visible)
	for i := m.modal.scroll; i < end; i++ {
		body += "\n  " + styAccent.Render(lines[i])
	}
	if len(lines) > visible {
		body += "\n" + styFaint.Render(fmt.Sprintf("↑↓ 滚动（%d/%d 行）", end, len(lines)))
	}
	return body, "enter 关闭"
}

// wrapText wraps plain text to width columns, breaking long words and
// respecting explicit newlines.
func wrapText(text string, width int) []string {
	if width < 4 {
		width = 4
	}
	var out []string
	for _, paragraph := range strings.Split(text, "\n") {
		if paragraph == "" {
			out = append(out, "")
			continue
		}
		var line []rune
		lineW := 0
		lastSpace := -1
		flush := func() {
			out = append(out, string(line))
			line = nil
			lineW = 0
			lastSpace = -1
		}
		for _, r := range paragraph {
			rw := ansi.StringWidth(string(r))
			if rw <= 0 {
				rw = 1
			}
			if lineW+rw > width {
				if lastSpace >= 0 {
					before := string(line[:lastSpace])
					after := strings.TrimLeft(string(line[lastSpace+1:]), " ")
					out = append(out, before)
					line = []rune(after)
					lineW = ansi.StringWidth(after)
					lastSpace = -1
					for i, rr := range line {
						if rr == ' ' {
							lastSpace = i
						}
					}
				} else {
					flush()
				}
			}
			if r == ' ' {
				lastSpace = len(line)
			}
			line = append(line, r)
			lineW += rw
		}
		if len(line) > 0 || paragraph == "" {
			out = append(out, string(line))
		}
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}
