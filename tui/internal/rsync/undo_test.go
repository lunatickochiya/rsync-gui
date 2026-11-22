package rsync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunatickochiya/rsync-gui/tui/internal/config"
)

func TestParseItemizedLine(t *testing.T) {
	cases := []struct {
		line  string
		ok    bool
		typ   byte
		flags string
		name  string
	}{
		{">f+++++++++ big.bin", true, '>', "+++++++++", "big.bin"},
		{">f.s....... a.txt", true, '>', ".s.......", "a.txt"},
		{"<f+++++++++ big.bin", true, '<', "+++++++++", "big.bin"},
		{"cd+++++++++ sub/", true, 'c', "+++++++++", "sub"},
		{"cL+++++++++ link => target", true, 'c', "+++++++++", "link"},
		{"hf+++++++++ name -> target", true, 'h', "+++++++++", "name"},
		{"*deleting   sub/old.txt", true, '*', "", "sub/old.txt"},
		{"*deleting   sub/", true, '*', "", "sub"},
		{">f+++++++++ >f+++++++++ odd", true, '>', "+++++++++", ">f+++++++++ odd"},
		{".d..t...... sub/", true, '.', "..t......", "sub"},
		{"big.bin", false, 0, "", ""},
		{"sending incremental file list", false, 0, "", ""},
	}
	for _, tc := range cases {
		typ, flags, name, ok := ParseItemizedLine(tc.line)
		if ok != tc.ok {
			t.Errorf("%q: ok = %v", tc.line, ok)
			continue
		}
		if !ok {
			continue
		}
		if typ != tc.typ || flags != tc.flags || name != tc.name {
			t.Errorf("%q = %q %q %q", tc.line, typ, flags, name)
		}
	}
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return "<missing>"
	}
	return string(data)
}

func TestUndoEngine(t *testing.T) {
	dest := t.TempDir()
	backup := filepath.Join(dest, ".rsync-gui-undo-test")
	mkdir := func(path string) {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mkdir(filepath.Join(dest, "d"))
	mkdir(filepath.Join(backup, "gone", "inner"))
	// A merge case: the directory still exists in the destination, so the old
	// files inside are moved back one by one (not the whole tree with one rename).
	mkdir(filepath.Join(backup, "merged"))
	mkdir(filepath.Join(dest, "merged"))
	writeFile(t, filepath.Join(dest, "d", "leftover.tmp"), "junk") // not in the record: a partial file
	writeFile(t, filepath.Join(backup, "gone", "inner", "file"), "restored")
	writeFile(t, filepath.Join(backup, "replaced"), "old version")
	writeFile(t, filepath.Join(dest, "replaced"), "new version")
	writeFile(t, filepath.Join(backup, "blocked"), "kept simple")
	writeFile(t, filepath.Join(backup, "merged", "file"), "old version")
	writeFile(t, filepath.Join(dest, "merged", "file"), "new version")

	rec := UndoRecord{
		Destination: dest,
		UndoDirName: ".rsync-gui-undo-test",
		NewEntries:  []string{"d", "d/x", "../escape", "/absolute", "missing"},
	}
	deletions, restorations, backupExists := UndoCounts(rec)
	if !backupExists || deletions != 1 || restorations != 7 {
		t.Fatalf("counts = %d %d %v", deletions, restorations, backupExists)
	}

	result := UndoExecute(context.Background(), rec, nil)
	// The unsafe names are skipped, everything else is undone - also the file
	// that the record does not know about, because it is inside a directory that it does.
	if result.Failed != 2 || result.Deleted != 2 || result.Restored != 7 {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Lstat(filepath.Join(dest, "d")); !os.IsNotExist(err) {
		t.Fatal("dest/d still exists")
	}
	if got := readFile(t, filepath.Join(dest, "gone", "inner", "file")); got != "restored" {
		t.Fatalf("gone/inner/file = %q", got)
	}
	if got := readFile(t, filepath.Join(dest, "replaced")); got != "old version" {
		t.Fatalf("replaced = %q", got)
	}
	if got := readFile(t, filepath.Join(dest, "merged", "file")); got != "old version" {
		t.Fatalf("merged/file = %q", got)
	}
	if _, err := os.Lstat(filepath.Join(backup, "merged")); !os.IsNotExist(err) {
		t.Fatal("backup/merged still exists")
	}
	if _, err := os.Lstat(backup); !os.IsNotExist(err) {
		t.Fatal("backup dir still exists")
	}
	if len(result.Problems) != 2 {
		t.Fatalf("problems = %v", result.Problems)
	}
}

func TestUndoRecordPersistence(t *testing.T) {
	dest := t.TempDir()
	rec := UndoRecord{
		Destination: dest,
		UndoDirName: ".rsync-gui-undo-test",
		NewEntries:  []string{"plain.txt", "non-utf8-\xff.txt"},
	}
	if err := UndoSave(rec); err != nil {
		t.Fatal(err)
	}
	loaded, ok := UndoLoad(rec.RecordPath())
	if !ok {
		t.Fatal("record not loaded")
	}
	if loaded.Destination != rec.Destination || loaded.UndoDirName != rec.UndoDirName {
		t.Fatalf("loaded = %+v", loaded)
	}
	if len(loaded.NewEntries) != 2 || loaded.NewEntries[0] != "plain.txt" || loaded.NewEntries[1] != "non-utf8-\xff.txt" {
		t.Fatalf("entries = %q", loaded.NewEntries)
	}
	if err := os.Remove(rec.RecordPath()); err != nil {
		t.Fatal(err)
	}
	if _, ok := UndoLoad(rec.RecordPath()); ok {
		t.Fatal("record loaded after removal")
	}
}

func TestUndoUsable(t *testing.T) {
	conflicting := []string{"-b", "--backup", "--backup-dir=/x", "-i", "--itemize-changes",
		"--out-format=%i", "-avb"}
	for _, extra := range conflicting {
		opts := config.DefaultOptions()
		opts.ExtraArguments = extra
		if opts.UndoUsable() {
			t.Errorf("%q: usable", extra)
		}
	}
	opts := config.DefaultOptions()
	opts.ExtraArguments = "-avz"
	if !opts.UndoUsable() {
		t.Error("-avz should not interfere")
	}
}

func TestCopyUndoEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync is not installed")
	}
	src := t.TempDir()
	dest := t.TempDir()

	// The destination before the run: a file that the run replaces, an extraneous
	// subtree that --delete removes, and a file that the run does not change.
	writeFile(t, filepath.Join(dest, "old.txt"), "old content")
	writeFile(t, filepath.Join(dest, "sub", "deep", "d.txt"), "deep")
	writeFile(t, filepath.Join(dest, "sub", "kept.txt"), "kept")
	writeFile(t, filepath.Join(dest, "same.txt"), "same")
	// The source: replaces old.txt, adds fresh.txt and the directory newdir.
	writeFile(t, filepath.Join(src, "old.txt"), strings.Repeat("n", 8*1024*1024))
	writeFile(t, filepath.Join(src, "same.txt"), "same")
	writeFile(t, filepath.Join(src, "fresh.txt"), "fresh")
	writeFile(t, filepath.Join(src, "newdir", "a.txt"), "aaa")
	writeFile(t, filepath.Join(src, "newdir", "deep", "b.txt"), "bbb")
	if err := os.Symlink("a.txt", filepath.Join(src, "newdir", "link")); err != nil {
		t.Fatal(err)
	}
	// Both sides of the unchanged file get the same fixed time, so that rsync
	// -a does not copy it again (it compares the times).
	sameTime := time.Unix(1_000_000_001, 0)
	for _, path := range []string{filepath.Join(dest, "same.txt"), filepath.Join(src, "same.txt")} {
		if err := os.Chtimes(path, sameTime, sameTime); err != nil {
			t.Fatal(err)
		}
	}

	opts := config.DefaultOptions()
	opts.Progress = "none"
	opts.ContentsOnly = true
	opts.DeleteExtraneous = true
	plan, err := BuildPlan(opts, []string{src}, dest)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Options.UndoDirName == "" {
		t.Fatal("no undo data planned")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var record *UndoRecord
	for ev := range Start(ctx, plan) {
		switch ev.Kind {
		case EventUndoAvailable:
			record = ev.UndoRecord
		case EventLog:
			if ev.Log.Level == LevelError {
				t.Errorf("rsync: %s", ev.Log.Text)
			}
		}
	}
	if record == nil {
		t.Fatal("no undo record after the run")
	}

	// The run changed the destination.
	if data, err := os.ReadFile(filepath.Join(dest, "old.txt")); err != nil || len(data) != 8*1024*1024 {
		t.Fatal("old.txt was not replaced")
	}
	if _, err := os.Lstat(filepath.Join(dest, "sub")); !os.IsNotExist(err) {
		t.Fatal("sub was not deleted")
	}

	// Undo restores the destination to its state before the run.
	deletions, restorations, _ := UndoCounts(*record)
	if deletions != 6 {
		t.Fatalf("deletions = %d", deletions)
	}
	if restorations != 5 {
		t.Fatalf("restorations = %d", restorations)
	}
	result := UndoExecute(ctx, *record, nil)
	if result.Failed != 0 || result.Deleted != 6 || result.Restored != 5 {
		t.Fatalf("result = %+v", result)
	}
	if got := readFile(t, filepath.Join(dest, "old.txt")); got != "old content" {
		t.Fatalf("old.txt = %q", got)
	}
	if got := readFile(t, filepath.Join(dest, "sub", "kept.txt")); got != "kept" {
		t.Fatalf("sub/kept.txt = %q", got)
	}
	if got := readFile(t, filepath.Join(dest, "same.txt")); got != "same" {
		t.Fatalf("same.txt = %q", got)
	}
	if _, err := os.Lstat(filepath.Join(dest, "fresh.txt")); !os.IsNotExist(err) {
		t.Fatal("fresh.txt still exists")
	}
	if _, err := os.Lstat(record.UndoDir()); !os.IsNotExist(err) {
		t.Fatal("backup dir still exists")
	}
}
