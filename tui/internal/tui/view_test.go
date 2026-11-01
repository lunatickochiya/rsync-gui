package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/lunatickochiya/rsync-gui/tui/internal/config"
	"github.com/lunatickochiya/rsync-gui/tui/internal/rsync"
)

func testModel() *Model {
	cfg := config.AppConfig{Options: config.DefaultOptions()}
	return New(config.DefaultOptions(), []string{"/tmp/src"}, "/tmp/dest", cfg, "", false)
}

func assertFits(t *testing.T, m *Model, width, height int) {
	t.Helper()
	m.width, m.height = width, height
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != height {
		t.Fatalf("%dx%d: %d lines", width, height, len(lines))
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("%dx%d: line %d is %d columns", width, height, i+1, w)
		}
	}
}

func TestSetupViewFits(t *testing.T) {
	for _, sz := range [][2]int{{120, 34}, {100, 30}, {80, 24}, {66, 18}} {
		m := testModel()
		assertFits(t, m, sz[0], sz[1])
		m.focus = focusOptions
		assertFits(t, m, sz[0], sz[1])
	}
}

func TestRunViewFits(t *testing.T) {
	m := testModel()
	m.screen = screenRun
	m.running = true
	m.state = rsync.State{
		Phase: "复制 1/2", Percent: 42, Transferred: "12.3M", Rate: "4.5MB/s",
		ETA: "0:00:12", FilesDone: 4, FilesTotal: 10, Speed: 4_700_000,
		CurrentFile:   "/home/user/photos/2024/very/long/path/image.jpg",
		TransferIndex: 1, TransferCount: 2, Step: 1, Steps: 2,
	}
	m.logs = []rsync.LogLine{{Level: rsync.LevelInfo, Text: "--- 复制 1/2 ---"}}
	m.speeds = []float64{1, 2, 3}
	for _, sz := range [][2]int{{120, 34}, {80, 24}, {66, 18}} {
		assertFits(t, m, sz[0], sz[1])
	}
	m.running = false
	m.done = true
	m.summary = &rsync.Summary{Total: 1, OK: 1, Verified: 1, Duration: 3_000_000_000}
	assertFits(t, m, 80, 24)
	assertFits(t, m, 100, 30)
}

func TestModalViewFits(t *testing.T) {
	m := testModel()
	m.openInput("source", "添加源", "路径", "/tmp/")
	m.completions = []string{"/tmp/a", "/tmp/b", "/tmp/c"}
	assertFits(t, m, 80, 24)
	m.openPresetPicker()
	assertFits(t, m, 80, 24)
	assertFits(t, m, 66, 18)
	m.openHelp()
	assertFits(t, m, 80, 24)
	m.modal = modalState{kind: modalMessage, title: "错误", body: strings.Repeat("很长的错误信息 ", 80)}
	assertFits(t, m, 80, 24)
}

func TestWrapText(t *testing.T) {
	lines := wrapText("hello world 这是一个很长的中文句子用于测试换行", 12)
	for _, line := range lines {
		if lipgloss.Width(line) > 12 {
			t.Fatalf("line too wide: %q", line)
		}
	}
	if len(lines) < 2 {
		t.Fatalf("expected wrapping: %v", lines)
	}
}

func TestPreviewCommand(t *testing.T) {
	m := testModel()
	cmd := m.previewCommand()
	if !strings.Contains(cmd, "rsync -avhP") || !strings.Contains(cmd, "/tmp/src /tmp/dest/") {
		t.Fatalf("preview = %q", cmd)
	}
	m.opts.SSHKey = "/tmp/key"
	m.dest = "nas:/backup"
	cmd = m.previewCommand()
	if !strings.Contains(cmd, "nas:/backup/") {
		t.Fatalf("preview = %q", cmd)
	}
}
