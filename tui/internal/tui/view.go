package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/lunatickochiya/rsync-gui/tui/internal/rsync"
)

var logoLines = []string{
	"█▀▀█ █▀▀ █ █ █▀▀█ █▀▀█",
	"█▄▄▀ ▀▀█ ▀▀█ █  █ █   ",
	"▀ ▀▀ ▀▀▀ ▀▀▀ ▀  ▀ ▀▀▀▀",
}

// View renders the whole application.
func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return "rsync tui"
	}
	if m.width < 66 || m.height < 18 {
		msg := styMuted.Render("终端太小") + "\n\n" +
			styText.Render(fmt.Sprintf("当前 %d×%d，至少需要 66×18。", m.width, m.height)) + "\n" +
			styMuted.Render("请放大窗口或缩小字体。")
		return lipgloss.NewStyle().
			Width(m.width).Height(m.height).Background(cBg).
			Align(lipgloss.Center, lipgloss.Center).Render(msg)
	}
	var body string
	if m.modal.kind != modalNone {
		body = m.renderModal()
	} else if m.screen == screenRun {
		body = m.renderRun()
	} else {
		body = m.renderSetup()
	}
	lines := strings.Split(body, "\n")
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	for i := range lines {
		lines[i] = truncate(lines[i], m.width)
	}
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	body = strings.Join(lines, "\n")
	return lipgloss.NewStyle().Width(m.width).Height(m.height).Background(cBg).Render(body)
}

func (m *Model) renderHeader() string {
	left := "  " + styAccentBold.Render("◆ rsync") + styMuted.Render(" tui")
	if m.running || m.done {
		phase := "传输"
		if m.done {
			phase = m.completionLabel()
		} else if m.state.Phase != "" {
			phase = m.state.Phase
		}
		left += styFaint.Render("  /  ") + styText.Render(truncate(phase, max(10, m.width/3)))
	} else {
		left += styFaint.Render("  /  ") + styMuted.Render("传输设置")
	}
	var rightParts []string
	if m.screen == screenRun {
		rightParts = append(rightParts, styMuted.Render(humanDuration(m.elapsed)))
		if m.state.TransferCount > 1 {
			rightParts = append(rightParts, styMuted.Render(fmt.Sprintf("%d/%d", m.state.TransferIndex, m.state.TransferCount)))
		}
	} else {
		rightParts = append(rightParts, styMuted.Render(m.rsyncVersion))
		if m.wsl {
			rightParts = append(rightParts, pill("WSL", cBlue))
		}
	}
	right := strings.Join(rightParts, "  ") + "  "
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		left = truncate(left, max(0, m.width-lipgloss.Width(right)-1))
		gap = max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right))
	}
	line := left + strings.Repeat(" ", gap) + right
	rule := styFaint.Render(strings.Repeat("─", max(0, m.width)))
	return line + "\n" + rule
}

func (m *Model) footerHints() string {
	if m.toast != "" {
		color := cGreen
		glyph := "✓ "
		if m.toastBad {
			color = cYellow
			glyph = "! "
		}
		return "  " + lipgloss.NewStyle().Foreground(color).Render(glyph+m.toast)
	}
	if m.screen == screenRun {
		if m.done {
			return "  " + hint("enter", "返回设置") + "   " + hint("r", "再运行") + "   " + hint("v", "命令") + "   " + hint("↑↓", "日志") + "   " + hint("q", "退出")
		}
		cancelHint := hint("esc", "取消")
		if m.cancelling {
			cancelHint = styYellow.Render("正在取消…")
		}
		return "  " + cancelHint + "   " + hint("↑↓", "滚动日志") + "   " + hint("end", "跟随") + "   " + hint("?", "帮助")
	}
	items := []string{
		hint("tab", "切换"),
		hint("a", "添加源"),
		hint("d", "目标"),
		hint("p", "预设"),
		hint("space", "切换"),
		hint("r", "开始"),
		hint("?", "帮助"),
		hint("q", "退出"),
	}
	if m.focus == focusSources {
		items = []string{
			hint("a", "添加"), hint("A", "批量"), hint("e", "编辑"), hint("x", "移除"),
			hint("d", "目标"), hint("p", "预设"), hint("r", "开始"), hint("?", "帮助"), hint("q", "退出"),
		}
	}
	line := "  " + strings.Join(items, styFaint.Render(" · "))
	if lipgloss.Width(line) <= m.width {
		return line
	}
	// Drop the least important hints until the line fits.
	for len(items) > 3 {
		items = append(items[:len(items)-2], items[len(items)-1])
		line = "  " + strings.Join(items, styFaint.Render(" · "))
		if lipgloss.Width(line) <= m.width {
			return line
		}
	}
	return truncate(line, m.width)
}

func (m *Model) renderFooter() string {
	rule := styFaint.Render(strings.Repeat("─", max(0, m.width)))
	return rule + "\n" + m.footerHints()
}

func (m *Model) logoBlock(width int) string {
	if width < 27 {
		return styAccentBold.Render("◆ rsync") + "\n" + styMuted.Render("terminal transfer")
	}
	var out []string
	for _, line := range logoLines {
		out = append(out, styFaint.Render(line))
	}
	out = append(out, styMuted.Render("终端里的 rsync 传输面板"))
	return strings.Join(out, "\n")
}

func (m *Model) renderSetup() string {
	bodyH := max(1, m.height-4)
	sidebarW := 30
	if m.width < 110 {
		sidebarW = 27
	}
	if m.width < 92 {
		sidebarW = 24
	}
	gap := 1
	mainW := m.width - sidebarW - gap
	sidebar := m.renderSidebar(sidebarW, bodyH)
	main := m.renderMain(mainW, bodyH)
	bodyLines := strings.Split(sidebar, "\n")
	mainLines := strings.Split(main, "\n")
	var joined []string
	sideStyle := lipgloss.NewStyle().Width(sidebarW)
	mainStyle := lipgloss.NewStyle().Width(mainW)
	for i := 0; i < bodyH; i++ {
		var left, right string
		if i < len(bodyLines) {
			left = bodyLines[i]
		}
		if i < len(mainLines) {
			right = mainLines[i]
		}
		left = truncate(left, sidebarW)
		right = truncate(right, mainW)
		joined = append(joined, sideStyle.Render(left)+strings.Repeat(" ", gap)+mainStyle.Render(right))
	}
	return m.renderHeader() + "\n" + strings.Join(joined, "\n") + "\n" + m.renderFooter()
}

func (m *Model) renderSidebar(width, height int) string {
	lines := strings.Split(m.logoBlock(width), "\n")
	for len(lines) < 5 {
		lines = append(lines, "")
	}
	lines = append(lines, "")
	titleColor := cMuted
	if m.focus == focusSources {
		titleColor = cAccent
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(titleColor).Bold(true).Render("源")+
		styFaint.Render(fmt.Sprintf("  %d", len(m.sources))))

	presetBlock := 4
	listHeight := max(1, height-len(lines)-presetBlock-1)
	if len(m.sources) == 0 {
		lines = append(lines, styFaint.Render("  还没有源"))
		if listHeight > 1 {
			lines = append(lines, "")
		}
	} else {
		m.clampSourceScroll(listHeight)
		for i := 0; i < listHeight; i++ {
			idx := m.sourceScroll + i
			if idx >= len(m.sources) {
				lines = append(lines, "")
				continue
			}
			selected := idx == m.sourceCursor && m.focus == focusSources
			lines = append(lines, m.renderSourceRow(idx, width, selected))
		}
	}
	lines = append(lines, "  "+keycap("a")+" "+styMuted.Render("添加源")+"  "+keycap("A")+" "+styMuted.Render("批量"))

	for len(lines) < height-presetBlock {
		lines = append(lines, "")
	}
	lines = append(lines, "")
	lines = append(lines, styMuted.Bold(true).Render("预设"))
	presetLabel := "自定义"
	if m.activePreset != "" {
		if p, ok := findPreset(m.presets, m.activePreset); ok {
			presetLabel = p.Label
		}
	}
	lines = append(lines, "  "+truncate(presetLabel, width-4))
	lines = append(lines, "  "+hint("p", "选择预设"))
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m *Model) clampSourceScroll(listHeight int) {
	if m.sourceCursor < m.sourceScroll {
		m.sourceScroll = m.sourceCursor
	}
	if m.sourceCursor >= m.sourceScroll+listHeight {
		m.sourceScroll = m.sourceCursor - listHeight + 1
	}
	maxScroll := max(0, len(m.sources)-listHeight)
	m.sourceScroll = clamp(m.sourceScroll, 0, maxScroll)
}

func (m *Model) renderSourceRow(idx, width int, selected bool) string {
	prefix := "  "
	if selected {
		prefix = styAccent.Render("▌") + " "
	}
	number := styFaint.Render(fmt.Sprintf("%d", idx+1))
	raw := m.sources[idx]
	display := raw
	if strings.HasPrefix(raw, "/") {
		display = displayPathShort(raw)
	}
	remote := strings.Contains(raw, ":") && !strings.HasPrefix(raw, "/")
	icon := "·"
	iconStyle := styFaint
	if remote {
		icon = "◈"
		iconStyle = styBlue
	}
	prefix += number + " " + iconStyle.Render(icon) + " "
	avail := width - lipgloss.Width(prefix) - 1
	text := truncate(display, max(1, avail))
	if selected {
		return prefix + styText.Render(text) + strings.Repeat(" ", max(0, width-lipgloss.Width(prefix)-lipgloss.Width(text)))
	}
	return prefix + styMuted.Render(text)
}

func displayPathShort(path string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if path == home {
			return "~"
		}
		if strings.HasPrefix(path, home+"/") {
			return "~" + path[len(home):]
		}
	}
	return path
}

func (m *Model) renderMain(width, height int) string {
	lines := make([]string, 0, height)
	// Title line.
	modeColor := cGreen
	if m.opts.RemoveSourceFiles {
		modeColor = cYellow
	}
	titleLeft := "  " + styBold.Render("传输设置") + "  " + pill(m.opts.ModeName(), modeColor)
	if m.opts.VerifyAfter && !m.opts.DryRun {
		titleLeft += " " + pill("校验", cBlue)
	}
	if m.opts.DryRun {
		titleLeft += " " + pill("试运行", cPurple)
	}
	preset := "自定义"
	if m.activePreset != "" {
		if p, ok := findPreset(m.presets, m.activePreset); ok {
			preset = p.Label
		}
	}
	right := styFaint.Render("预设 ") + styMuted.Render(truncate(preset, max(8, width/3))) + "  "
	gap := width - lipgloss.Width(titleLeft) - lipgloss.Width(right)
	if gap < 1 {
		titleLeft = truncate(titleLeft, max(0, width-lipgloss.Width(right)-1))
		gap = max(1, width-lipgloss.Width(titleLeft)-lipgloss.Width(right))
	}
	lines = append(lines, titleLeft+strings.Repeat(" ", gap)+right)

	// Destination box.
	lines = append(lines, m.renderDest(width)...)
	lines = append(lines, "")

	// Options title.
	focusColor := cMuted
	if m.focus == focusOptions {
		focusColor = cAccent
	}
	optTitle := "  " + lipgloss.NewStyle().Foreground(focusColor).Bold(true).Render("选项") +
		styFaint.Render(fmt.Sprintf("  %d 项", len(optionRows())))
	rows := optionRows()
	scrollHint := ""
	if m.optionScroll > 0 {
		scrollHint += "↑ "
	}
	if m.optionScroll+max(1, height-8) < len(rows) {
		scrollHint += "↓"
	}
	optTitleRight := styFaint.Render(scrollHint) + "  "
	optGap := width - lipgloss.Width(optTitle) - lipgloss.Width(optTitleRight)
	if optGap < 1 {
		optTitle = truncate(optTitle, max(0, width-lipgloss.Width(optTitleRight)-1))
		optGap = max(1, width-lipgloss.Width(optTitle)-lipgloss.Width(optTitleRight))
	}
	lines = append(lines, optTitle+strings.Repeat(" ", optGap)+optTitleRight)

	listHeight := max(1, height-8)
	m.clampOptionScroll(listHeight)
	for i := 0; i < listHeight; i++ {
		idx := m.optionScroll + i
		if idx >= len(rows) {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, m.renderOptionRow(rows[idx], idx, width))
	}
	// Command preview.
	cmd := m.previewCommand()
	lines = append(lines, "  "+styFaint.Render("命令 ")+hint("v", "查看")+"  "+truncateMiddle(cmd, max(1, width-18)))
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderDest(width int) []string {
	innerW := max(20, width-2)
	label := styMuted.Render("目标")
	value := m.dest
	if value == "" {
		value = "尚未设置"
	}
	style := styAccent
	if m.dest == "" {
		style = styFaint
	}
	hintText := hint("d", "编辑")
	textW := innerW - 2 - lipgloss.Width(hintText)
	text := label + "  " + style.Render(truncateMiddle(value, max(4, textW-6)))
	gap := innerW - 2 - lipgloss.Width(text) - lipgloss.Width(hintText)
	if gap < 1 {
		text = truncate(text, max(1, innerW-2-lipgloss.Width(hintText)-1))
		gap = max(1, innerW-2-lipgloss.Width(text)-lipgloss.Width(hintText))
	}
	content := " " + text + strings.Repeat(" ", gap) + hintText + " "
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cBorder).
		Width(innerW).
		Render(content)
	return strings.Split(box, "\n")
}

func (m *Model) clampOptionScroll(listHeight int) {
	if m.optionCursor < m.optionScroll {
		m.optionScroll = m.optionCursor
	}
	if m.optionCursor >= m.optionScroll+listHeight {
		m.optionScroll = m.optionCursor - listHeight + 1
	}
	rows := optionRows()
	maxScroll := max(0, len(rows)-listHeight)
	m.optionScroll = clamp(m.optionScroll, 0, maxScroll)
}

func (m *Model) renderOptionRow(row optionRow, idx, width int) string {
	if row.kind == rowHeader {
		label := styMuted.Bold(true).Render("  " + row.title + " ")
		line := styFaint.Render(strings.Repeat("─", max(0, width-lipgloss.Width(label)-2)))
		return label + line
	}
	selected := idx == m.optionCursor && m.focus == focusOptions
	state := m.optionState(row.id)
	indent := "  "
	if row.indent {
		indent = "    "
	}
	mark := styFaint.Render("[ ]")
	if state.checked {
		mark = styGreen.Render("[x]")
	}
	if row.kind == rowChoice {
		mark = styFaint.Render("‹") + styAccent.Render(state.value) + styFaint.Render("›")
	}
	if row.kind == rowText {
		if state.value == "" {
			mark = styFaint.Render("[ ]")
		} else if state.checked {
			mark = styGreen.Render("[x]")
		} else {
			mark = styFaint.Render("[ ]")
		}
	}
	if row.kind == rowAction {
		mark = styAccent.Render("↺")
	}
	titleStyle := styText
	if state.disabled {
		titleStyle = styFaint
	}
	title := titleStyle.Render(row.title)
	flag := ""
	if row.flag != "" {
		flag = styFaint.Render(row.flag)
	}
	value := ""
	if row.kind == rowText {
		if state.value == "" {
			value = styFaint.Render("未设置")
		} else {
			value = styMuted.Render(truncateMiddle(state.value, max(8, width/3)))
		}
	} else if row.kind == rowChoice {
		value = ""
	} else if state.note != "" {
		value = styYellow.Render(state.note)
	}
	left := indent + mark + " " + title
	right := ""
	if value != "" {
		right = value
	}
	if flag != "" {
		if right != "" {
			right += "  " + flag
		} else {
			right = flag
		}
	}
	space := width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if space < 1 {
		left = truncate(left, max(1, width-lipgloss.Width(right)-3))
		space = max(1, width-lipgloss.Width(left)-lipgloss.Width(right)-2)
	}
	line := left + strings.Repeat(" ", space) + right + "  "
	if selected {
		bar := lipgloss.NewStyle().Foreground(cAccent).Background(cPanelHigh).Render("▌")
		body := strings.TrimPrefix(line, " ")
		rendered := lipgloss.NewStyle().Background(cPanelHigh).Render(body)
		// Lipgloss resets the background after each coloured segment; keep the
		// selection background across the whole row.
		rendered = strings.ReplaceAll(rendered, "\x1b[0m", "\x1b[39m\x1b[48;2;30;30;30m")
		rendered = strings.ReplaceAll(rendered, "\x1b[49m", "\x1b[39m\x1b[48;2;30;30;30m")
		return bar + rendered + "\x1b[0m"
	}
	return line
}

func (m *Model) renderRun() string {
	bodyH := max(1, m.height-4)
	lines := make([]string, 0, bodyH)
	status := m.renderRunStatus(m.width)
	lines = append(lines, status...)
	// Summary or hints after the log.
	summaryH := 0
	if m.summary != nil {
		summaryH = 3
	}
	logH := max(1, bodyH-len(status)-summaryH-1)
	lines = append(lines, m.renderLogs(m.width, logH)...)
	if m.summary != nil {
		lines = append(lines, m.renderSummary(m.width)...)
	}
	if len(lines) > bodyH {
		lines = lines[:bodyH]
	}
	for len(lines) < bodyH {
		lines = append(lines, "")
	}
	return m.renderHeader() + "\n" + strings.Join(lines, "\n") + "\n" + m.renderFooter()
}

func (m *Model) renderRunStatus(width int) []string {
	s := m.state
	lines := make([]string, 0, 6)
	modeColor := cGreen
	if m.opts.RemoveSourceFiles {
		modeColor = cYellow
	}
	phase := s.Phase
	phaseColor := modeColor
	if m.done {
		phase = m.completionLabel()
		phaseColor = cGreen
		if m.summary != nil {
			if m.summary.Warnings > 0 && m.summary.Failed == 0 && m.summary.Differences == 0 && m.summary.VerifyFailed == 0 {
				phaseColor = cYellow
			}
			if !m.summary.Success() {
				phaseColor = cYellow
			}
			if m.summary.Failed > 0 || m.summary.Aborted || m.summary.Differences > 0 || m.summary.VerifyFailed > 0 || m.summary.RemovalFailed > 0 {
				phaseColor = cRed
			}
		}
	} else if strings.Contains(phase, "校验") {
		phaseColor = cBlue
	} else if strings.Contains(phase, "删除") {
		phaseColor = cYellow
	}
	if phase == "" {
		if m.done {
			phase = "完成"
		} else {
			phase = "准备中"
		}
	}
	left := "  " + pill(phase, phaseColor)
	if m.opts.RemoveSourceFiles && !strings.Contains(phase, "删除") {
		left += " " + pill("移动", cYellow)
	}
	if m.cancelling {
		left += "  " + pill("取消中", cRed)
	}
	right := ""
	if m.done {
		right = styMuted.Render(humanDuration(m.summaryDuration()))
	} else {
		right = styMuted.Render(humanDuration(m.elapsed))
	}
	right += "  "
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		left = truncate(left, max(1, width-lipgloss.Width(right)-1))
		gap = max(1, width-lipgloss.Width(left)-lipgloss.Width(right))
	}
	lines = append(lines, left+strings.Repeat(" ", gap)+right)

	percentText := fmt.Sprintf("%.0f%%", s.Percent)
	if s.Indeterminate && !m.done {
		spin := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
		label := "处理中"
		if strings.Contains(phase, "校验") {
			label = "校验中"
		} else if strings.Contains(phase, "删除") {
			label = "删除中"
		} else if strings.Contains(phase, "复制") {
			label = "复制中"
		}
		percentText = string(spin[m.frame%len(spin)]) + "  " + label
	}
	speed := humanSpeed(s.Speed)
	if speed == "" {
		speed = s.Rate
	}
	big := lipgloss.NewStyle().Foreground(cAccent).Bold(true).Render(percentText)
	stats := ""
	if speed != "" {
		stats += styText.Render(speed)
	}
	if s.ETA != "" && s.ETA != "0:00:00" {
		stats += styMuted.Render("  ETA ") + styText.Render(s.ETA)
	}
	if s.FilesTotal > 0 {
		stats += styMuted.Render(fmt.Sprintf("  文件 %d/%d", s.FilesDone, s.FilesTotal))
	}
	if stats == "" && !m.done {
		stats = styFaint.Render("等待 rsync 输出…")
	}
	line2 := "  " + big + "  " + stats
	lines = append(lines, line2)
	barWidth := max(10, width-4)
	lines = append(lines, "  "+gradientBar(s.Percent, barWidth, s.Indeterminate && !m.done, m.frame))
	detail := "  "
	if s.Transferred != "" {
		detail += styMuted.Render("已传输 ") + styText.Render(s.Transferred)
	}
	if s.Rate != "" && s.Rate != speed {
		detail += styMuted.Render("  ·  ") + styText.Render(s.Rate)
	}
	if s.CurrentFile != "" {
		detail += styMuted.Render("  ·  ") + styFaint.Render(truncateMiddle(s.CurrentFile, max(10, width/2)))
	} else if !m.done {
		text := "等待 rsync 输出…"
		if strings.Contains(phase, "校验") {
			text = "读取两端并比较校验和；不会修改文件。"
		} else if strings.Contains(phase, "删除") {
			text = "校验通过，正在删除已传输的源文件。"
		} else if m.opts.Progress == "none" {
			text = "进度输出已关闭（可在选项中开启）。"
		}
		detail += styFaint.Render(text)
	}
	lines = append(lines, detail)
	spark := sparkline(m.speeds, max(10, width-8))
	if spark == "" {
		spark = styFaint.Render(strings.Repeat("▁", max(10, min(40, width-8))))
	}
	lines = append(lines, "  "+styFaint.Render("速度 ")+spark)
	return lines
}

func (m *Model) summaryDuration() time.Duration {
	if m.summary != nil {
		return m.summary.Duration
	}
	return m.elapsed
}

func (m *Model) renderLogs(width, height int) []string {
	title := "  " + styMuted.Bold(true).Render("日志") + styFaint.Render(fmt.Sprintf("  %d 行", len(m.logs)))
	if len(m.logs) > 0 {
		if m.logScroll == 0 {
			title += styFaint.Render("  ·  跟随")
		} else {
			title += styYellow.Render(fmt.Sprintf("  ·  向上 %d 行", m.logScroll))
		}
	}
	title += styFaint.Render("    ↑↓ 滚动 · end 跟随")
	lines := []string{title}
	end := len(m.logs) - m.logScroll
	if end < 0 {
		end = 0
	}
	start := max(0, end-height)
	end = min(len(m.logs), start+height)
	for i := start; i < end; i++ {
		lines = append(lines, m.renderLogLine(m.logs[i], width))
	}
	for len(lines) < height+1 {
		lines = append(lines, "")
	}
	return lines
}

func (m *Model) renderLogLine(entry rsync.LogLine, width int) string {
	color := cMuted
	glyph := "·"
	switch entry.Level {
	case rsync.LevelInfo:
		color = cBlue
		glyph = "›"
	case rsync.LevelSuccess:
		color = cGreen
		glyph = "✓"
	case rsync.LevelWarning:
		color = cYellow
		glyph = "!"
	case rsync.LevelError:
		color = cRed
		glyph = "✗"
	}
	stamp := styFaint.Render(entry.Time.Format("15:04:05"))
	prefix := "  " + stamp + "  " + lipgloss.NewStyle().Foreground(color).Render(glyph) + " "
	avail := width - lipgloss.Width(prefix) - 1
	return prefix + lipgloss.NewStyle().Foreground(color).Render(truncate(entry.Text, max(1, avail)))
}

func (m *Model) completionLabel() string {
	if m.summary == nil {
		return "已结束"
	}
	s := m.summary
	switch {
	case s.Aborted:
		return "失败"
	case s.Cancelled:
		return "已取消"
	case s.Differences > 0:
		return "有差异"
	case s.Failed > 0 || s.VerifyFailed > 0 || s.RemovalFailed > 0:
		return "失败"
	case s.Warnings > 0:
		return "完成（警告）"
	default:
		return "完成"
	}
}

func (m *Model) renderSummary(width int) []string {
	s := m.summary
	status := "完成"
	color := cGreen
	glyph := "✓"
	if s.Cancelled {
		status, color, glyph = "已取消", cYellow, "!"
	} else if s.Failed > 0 || s.VerifyFailed > 0 || s.Aborted || s.Differences > 0 {
		status, color, glyph = "有问题", cRed, "✗"
	}
	if s.Aborted || s.Err != "" {
		status, color, glyph = "失败", cRed, "✗"
	}
	line1 := "  " + lipgloss.NewStyle().Foreground(color).Bold(true).Render(glyph+" "+status) +
		styMuted.Render("  ·  "+humanDuration(s.Duration))
	line2 := "  " + styMuted.Render(fmt.Sprintf("传输 %d 个 · 完成 %d · 警告 %d · 失败 %d", s.Total, s.OK, s.Warnings, s.Failed))
	if m.opts.CanVerify() {
		line2 += styMuted.Render(fmt.Sprintf("  ·  校验通过 %d · 差异 %d", s.Verified, s.Differences))
	}
	if s.RemovalFailed > 0 {
		line2 += styRed.Render(fmt.Sprintf("  ·  删除失败 %d", s.RemovalFailed))
	}
	line3 := ""
	if s.Err != "" {
		line3 = "  " + styRed.Render(truncate(s.Err, width-4))
	} else if s.Differences > 0 {
		line3 = "  " + styRed.Render("目标内容与源不一致，请查看日志中的“差异”行。")
	} else if s.Cancelled {
		line3 = "  " + styMuted.Render("任务已取消。")
	} else if s.Success() {
		line3 = "  " + styGreen.Render("全部完成。")
	}
	return []string{line1, line2, line3}
}
