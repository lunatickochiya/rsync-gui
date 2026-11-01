package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lunatickochiya/rsync-gui/tui/internal/rsync"
)

func mouseMsg(x, y int, button tea.MouseButton) tea.MouseMsg {
	return tea.MouseMsg{Action: tea.MouseActionPress, Button: button, X: x, Y: y}
}

func TestMouseClickTogglesOption(t *testing.T) {
	m := testModel()
	m.width, m.height = 100, 30
	m.View() // populate hit areas
	if len(m.hits.optionRows) == 0 {
		t.Fatal("no option hit areas")
	}
	row := m.hits.optionRows[0]
	if !m.opts.Archive {
		t.Fatal("archive should start on")
	}
	m.Update(mouseMsg(row.rect.x+5, row.rect.y, tea.MouseButtonLeft))
	if m.opts.Archive {
		t.Fatal("click did not toggle archive mode")
	}
	if m.focus != focusOptions {
		t.Fatal("click did not focus options")
	}
}

func TestMouseClickDestination(t *testing.T) {
	m := testModel()
	m.width, m.height = 100, 30
	m.View()
	if m.hits.dest.w == 0 {
		t.Fatal("no destination hit area")
	}
	m.Update(mouseMsg(m.hits.dest.x+5, m.hits.dest.y+1, tea.MouseButtonLeft))
	if m.modal.kind != modalInput || m.modal.field != "dest" {
		t.Fatalf("destination editor not opened: %+v", m.modal)
	}
}

func TestMouseClickPreset(t *testing.T) {
	m := testModel()
	m.width, m.height = 120, 34
	m.openPresetPicker()
	m.View()
	if len(m.hits.presetItemLines) < 2 {
		t.Fatalf("preset hit lines = %v", m.hits.presetItemLines)
	}
	y := m.hits.modalBox.y + 2 + m.hits.presetItemLines[1]
	x := m.hits.modalBox.x + 10
	m.Update(mouseMsg(x, y, tea.MouseButtonLeft))
	if m.modal.kind != modalNone {
		t.Fatalf("preset modal did not close: %+v", m.modal)
	}
	if m.activePreset == "" {
		t.Fatal("preset was not applied")
	}
}

func TestMouseWheelDirection(t *testing.T) {
	m := testModel()
	m.width, m.height = 100, 30
	m.View()
	if m.hits.optionArea.w == 0 {
		t.Fatal("no option area")
	}
	start := m.optionCursor
	x := m.hits.optionArea.x + 5
	y := m.hits.optionArea.y + 2
	m.Update(mouseMsg(x, y, tea.MouseButtonWheelDown))
	if m.optionCursor <= start {
		t.Fatalf("wheel down did not move down: %d -> %d", start, m.optionCursor)
	}
	down := m.optionCursor
	m.Update(mouseMsg(x, y, tea.MouseButtonWheelUp))
	if m.optionCursor >= down {
		t.Fatalf("wheel up did not move up: %d -> %d", down, m.optionCursor)
	}
}

func TestMouseWheelScrollsRunLog(t *testing.T) {
	m := testModel()
	m.width, m.height = 100, 30
	m.screen = screenRun
	m.logs = []rsync.LogLine{
		{Text: "one"}, {Text: "two"}, {Text: "three"}, {Text: "four"}, {Text: "five"},
	}
	m.Update(mouseMsg(40, 15, tea.MouseButtonWheelUp))
	if m.logScroll != 3 {
		t.Fatalf("wheel up logScroll = %d, want 3", m.logScroll)
	}
	m.Update(mouseMsg(40, 15, tea.MouseButtonWheelDown))
	if m.logScroll != 0 {
		t.Fatalf("wheel down logScroll = %d, want 0", m.logScroll)
	}
}
