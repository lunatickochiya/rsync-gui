package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.modal.kind {
	case modalInput:
		return m.handleInputKey(msg)
	case modalBulk:
		return m.handleBulkKey(msg)
	case modalPresets:
		return m.handlePresetKey(msg)
	case modalConfirm:
		return m.handleConfirmKey(msg)
	case modalHelp:
		return m.handleHelpKey(msg)
	case modalMessage:
		return m.handleMessageKey(msg)
	case modalCommand:
		return m.handleMessageKey(msg)
	}
	return m, nil
}

func (m *Model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.input.Blur()
		m.modal = modalState{}
		return m, nil
	case "enter":
		ok, errText := m.commitInput(m.input.Value())
		if !ok {
			m.modal.err = errText
			return m, nil
		}
		m.input.Blur()
		return m, nil
	case "tab":
		switch m.modal.field {
		case "source", "source-edit":
			m.completeInput(false)
		case "dest":
			m.completeInput(true)
		case "import-settings", "export-settings":
			m.completeInput(false)
		}
		return m, nil
	case "ctrl+n", "alt+down":
		if m.modal.field == "dest" {
			m.cycleRecent(1)
		}
		return m, nil
	case "ctrl+p", "alt+up":
		if m.modal.field == "dest" {
			m.cycleRecent(-1)
		}
		return m, nil
	}
	m.completions = nil
	m.completionBase = ""
	m.completionIdx = -1
	m.modal.err = ""
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) handleBulkKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.bulk.Blur()
		m.modal = modalState{}
		return m, nil
	case "ctrl+s":
		m.commitBulk()
		return m, nil
	}
	var cmd tea.Cmd
	m.bulk, cmd = m.bulk.Update(msg)
	return m, cmd
}

func (m *Model) handlePresetKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.modal = modalState{}
	case "up", "k":
		if m.modal.cursor > 0 {
			m.modal.cursor--
		}
	case "down", "j":
		if m.modal.cursor < len(m.modal.items)-1 {
			m.modal.cursor++
		}
	case "home", "g":
		m.modal.cursor = 0
	case "end", "G":
		m.modal.cursor = len(m.modal.items) - 1
	case "enter":
		if m.modal.cursor >= 0 && m.modal.cursor < len(m.modal.items) {
			m.applyPreset(m.modal.items[m.modal.cursor])
		}
	}
	return m, nil
}

func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "n":
		if m.pendingPlan != nil {
			m.pendingPlan.Cleanup()
			m.pendingPlan = nil
		}
		m.modal = modalState{}
	case "enter", "y":
		action := m.modal.action
		m.modal = modalState{}
		switch action {
		case "start-plan":
			if m.pendingPlan != nil {
				return m, m.startPlan(m.pendingPlan)
			}
		case "cancel-run":
			m.cancelRun()
		case "clear-sources":
			m.sources = nil
			m.sourceCursor = 0
			m.setToast("源列表已清空", false)
		case "reset-options":
			m.resetOptions()
		}
	}
	return m, nil
}

func (m *Model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter", "?":
		m.modal = modalState{}
	case "up", "k":
		m.modal.scroll = max(0, m.modal.scroll-1)
	case "down", "j":
		m.modal.scroll++
	case "pgup":
		m.modal.scroll = max(0, m.modal.scroll-8)
	case "pgdown":
		m.modal.scroll += 8
	case "home", "g":
		m.modal.scroll = 0
	}
	return m, nil
}

func (m *Model) handleMessageKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter":
		m.modal = modalState{}
	case "up", "k":
		m.modal.scroll = max(0, m.modal.scroll-1)
	case "down", "j":
		m.modal.scroll++
	case "pgup":
		m.modal.scroll = max(0, m.modal.scroll-8)
	case "pgdown":
		m.modal.scroll += 8
	}
	return m, nil
}

func (m *Model) cycleRecent(delta int) {
	candidates := m.recentCandidates()
	if len(candidates) == 0 {
		return
	}
	m.recentIdx = (m.recentIdx + delta + len(candidates)) % len(candidates)
	m.input.SetValue(candidates[m.recentIdx])
	m.input.CursorEnd()
	m.completions = nil
}

func (m *Model) recentCandidates() []string {
	var out []string
	if m.dest != "" {
		out = append(out, m.dest)
	}
	for _, dest := range m.cfg.RecentDestinations {
		found := false
		for _, existing := range out {
			if existing == dest {
				found = true
				break
			}
		}
		if !found {
			out = append(out, dest)
		}
	}
	return out
}

func (m *Model) completeInput(dirsOnly bool) {
	value := m.input.Value()
	if strings.Contains(value, "://") {
		return
	}
	if !strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "~") && !strings.HasPrefix(value, ".") &&
		strings.Contains(strings.SplitN(value, "/", 2)[0], ":") {
		return // remote rsync path
	}
	if len(m.completions) > 0 {
		m.completionIdx = (m.completionIdx + 1) % len(m.completions)
		m.input.SetValue(m.completions[m.completionIdx])
		m.input.CursorEnd()
		return
	}
	candidates := pathCandidates(value, dirsOnly)
	if len(candidates) == 0 {
		return
	}
	if len(candidates) == 1 {
		m.input.SetValue(candidates[0])
		m.input.CursorEnd()
		return
	}
	common := commonPrefix(candidates)
	if common != "" && common != value {
		m.input.SetValue(common)
		m.input.CursorEnd()
	}
	m.completions = candidates
	m.completionBase = m.input.Value()
	m.completionIdx = -1
}

func pathCandidates(value string, dirsOnly bool) []string {
	value = expandUser(value)
	dirPart := "."
	base := ""
	displayDir := "."
	if value != "" {
		if strings.HasSuffix(value, string(filepath.Separator)) {
			dirPart = value
			displayDir = value
		} else {
			dirPart = filepath.Dir(value)
			displayDir = filepath.Dir(value)
			base = filepath.Base(value)
		}
	}
	entries, err := os.ReadDir(dirPart)
	if err != nil {
		return nil
	}
	var out []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, base) {
			continue
		}
		if dirsOnly && !entry.IsDir() {
			continue
		}
		candidate := name
		if displayDir != "." {
			candidate = filepath.Join(displayDir, name)
		}
		if entry.IsDir() {
			candidate += string(filepath.Separator)
		}
		out = append(out, unexpandUser(candidate))
	}
	sort.Strings(out)
	return out
}

func commonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}
	prefix := []rune(values[0])
	for _, v := range values[1:] {
		r := []rune(v)
		n := len(prefix)
		if len(r) < n {
			n = len(r)
		}
		i := 0
		for i < n && prefix[i] == r[i] {
			i++
		}
		prefix = prefix[:i]
		if len(prefix) == 0 {
			break
		}
	}
	return string(prefix)
}

func expandUser(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func unexpandUser(path string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if path == home {
			return "~"
		}
		if strings.HasPrefix(path, home+string(filepath.Separator)) {
			return "~" + path[len(home):]
		}
	}
	return path
}
