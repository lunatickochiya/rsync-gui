package tui

import "testing"

func TestEditSource(t *testing.T) {
	m := testModel()
	m.sources = []string{"/tmp/src", "/tmp/other"}
	m.sourceCursor = 1

	// 'e' opens the input dialog with the exact stored path of the entry.
	m.openSourceEdit()
	if m.modal.field != "source-edit" {
		t.Fatalf("field = %q", m.modal.field)
	}
	if got := m.input.Value(); got != "/tmp/other" {
		t.Fatalf("prefill = %q", got)
	}

	// A changed path replaces that entry in place; the others stay.
	m.input.SetValue("/tmp/changed")
	if ok, errText := m.commitInput(m.input.Value()); !ok {
		t.Fatalf("commit: %s", errText)
	}
	if len(m.sources) != 2 || m.sources[0] != "/tmp/src" || m.sources[1] != "/tmp/changed" {
		t.Fatalf("sources = %v", m.sources)
	}

	// An invalid path is refused; the entry keeps its value.
	m.sourceCursor = 0
	m.openSourceEdit()
	m.input.SetValue("file://nas/x") // a remote file:// host is not supported
	if ok, errText := m.commitInput(m.input.Value()); ok {
		t.Fatalf("invalid path accepted: %q", errText)
	}
	if m.sources[0] != "/tmp/src" {
		t.Fatalf("sources = %v", m.sources)
	}

	// Editing a remote source keeps its stored rsync form.
	m.sources[1] = "user@nas:/volume1/photos"
	m.sourceCursor = 1
	m.openSourceEdit()
	m.input.SetValue("user@nas:/volume1/new")
	if ok, errText := m.commitInput(m.input.Value()); !ok {
		t.Fatalf("commit remote: %s", errText)
	}
	if m.sources[1] != "user@nas:/volume1/new" {
		t.Fatalf("remote source = %q", m.sources[1])
	}
}
