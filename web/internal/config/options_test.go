package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultCopyArgs(t *testing.T) {
	o := DefaultOptions()
	got := strings.Join(o.CopyArgs(0), " ")
	want := "-avhP"
	if got != want {
		t.Fatalf("CopyArgs = %q, want %q", got, want)
	}
}

func TestBuildArgsVariants(t *testing.T) {
	o := DefaultOptions()
	o.Progress = "overall"
	got := strings.Join(o.CopyArgs(0), " ")
	if want := "-avh --partial --info=progress2"; got != want {
		t.Fatalf("overall = %q, want %q", got, want)
	}

	o = DefaultOptions()
	o.Archive = false
	o.Perms, o.Group, o.Owner, o.Devices = false, false, false, false
	got = strings.Join(o.CopyArgs(0), " ")
	if want := "-rltvhP"; got != want {
		t.Fatalf("portable = %q, want %q", got, want)
	}

	o = DefaultOptions()
	o.Existing = "ignore-existing"
	o.RemoveSourceFiles = true
	o.Checksum = true
	got = strings.Join(o.CopyArgs(0), " ")
	if want := "-avhPc --ignore-existing --remove-source-files"; got != want {
		t.Fatalf("ignore+move = %q, want %q", got, want)
	}

	o = DefaultOptions()
	o.Archive = false
	o.HardLinks, o.ACLs, o.Xattrs, o.Checksum, o.Compress = true, true, true, true, true
	o.OneFileSystem = true
	o.Existing = "skip-newer"
	got = o.SummaryFlags()
	if want := "-rlptgoDHAXcxuz"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func TestRemoteShellArgs(t *testing.T) {
	o := DefaultOptions()
	o.SSHKey = "/home/me/key with space"
	o.UseRemoteShell = true
	o.RemoteShell = "ssh -o BatchMode=yes"
	args := strings.Join(o.CopyArgs(2222), " ")
	if !strings.Contains(args, "-e") || !strings.Contains(args, "-p 2222") || !strings.Contains(args, "BatchMode") {
		t.Fatalf("remote shell missing: %q", args)
	}
	if !strings.Contains(args, `-i '/home/me/key with space'`) {
		t.Fatalf("key not quoted: %q", args)
	}
}

func TestVerifyArgs(t *testing.T) {
	o := DefaultOptions()
	o.Checksum = true
	o.VerifyAfter = true
	o.ExcludeFile = "~/exclude.txt"
	got := strings.Join(o.VerifyArgs(0), " ")
	if !strings.HasPrefix(got, "-nrlci --out-format=%i %n%L") {
		t.Fatalf("verify args = %q", got)
	}
	if !strings.Contains(got, "--exclude-from=") {
		t.Fatalf("exclude missing: %q", got)
	}
}

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`--exclude='*.tmp' --mkpath`, []string{"--exclude=*.tmp", "--mkpath"}},
		{`-e "ssh -p 22" --x=a\ b`, []string{"-e", "ssh -p 22", "--x=a b"}},
		{`'' "a b"`, []string{"", "a b"}},
		{`a\ b`, []string{`a b`}},
	}
	for _, tc := range cases {
		got, err := SplitArgs(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Fatalf("%q => %#v, want %#v", tc.in, got, tc.want)
		}
	}
	if _, err := SplitArgs(`"unterminated`); err == nil {
		t.Fatal("expected error")
	}
}

func TestSettingsFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	o := DefaultOptions()
	o.Checksum = true
	o.VerifyAfter = true
	o.Progress = "overall"
	if err := SaveSettingsFile(path, "测试", o); err != nil {
		t.Fatal(err)
	}
	loaded, name, err := LoadSettingsFile(path, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if name != "测试" || !loaded.Checksum || !loaded.VerifyAfter || loaded.Progress != "overall" {
		t.Fatalf("loaded = %+v name=%q", loaded, name)
	}
}

func TestSettingsFileMissingKeysKeepBase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "partial.json")
	data := `{"format":"rsync-gui-settings","version":1,"options":{"compress":true}}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	base := DefaultOptions()
	base.VerifyAfter = true
	loaded, _, err := LoadSettingsFile(path, base)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.VerifyAfter || !loaded.Compress || !loaded.Archive {
		t.Fatalf("missing keys not preserved: %+v", loaded)
	}
}

func TestPresets(t *testing.T) {
	items := AvailablePresets()
	if len(items) < 6 {
		t.Fatalf("expected built-in presets, got %d", len(items))
	}
	p, ok := FindPreset("rsync-copy-portable")
	if !ok {
		t.Fatal("portable preset not found")
	}
	if p.Label != "复制到 FAT/NTFS/NAS (-rlt)" {
		t.Fatalf("label = %q", p.Label)
	}
	args := strings.Join(p.Options.CopyArgs(0), " ")
	if args != "-rltvhP" {
		t.Fatalf("portable args = %q", args)
	}
}

func TestMergePresetKeepsUserFields(t *testing.T) {
	base := DefaultOptions()
	base.Program = "/opt/rsync"
	base.SSHKey = "~/.ssh/id_ed25519"
	base.ExtraArguments = "--exclude='*.tmp'"
	p, ok := FindPreset("rsync-move")
	if !ok {
		t.Fatal("move preset missing")
	}
	merged := MergePreset(base, p.Options)
	if merged.Program != base.Program || merged.SSHKey != base.SSHKey || merged.ExtraArguments != base.ExtraArguments {
		t.Fatalf("user fields lost: %+v", merged)
	}
	if !merged.RemoveSourceFiles {
		t.Fatalf("preset options missing: %+v", merged)
	}
}

func TestValidateConflicts(t *testing.T) {
	o := DefaultOptions()
	o.DeleteExtraneous = true
	o.RemoveSourceFiles = true
	if err := o.Validate(); err == nil {
		t.Fatal("expected --delete conflict")
	}
	o = DefaultOptions()
	o.UseBwLimit = true
	o.BwLimit = "1..5M"
	if err := o.Validate(); err == nil {
		t.Fatal("expected bwlimit error")
	}
}

func TestShellQuote(t *testing.T) {
	if got := ShellQuote("simple/path"); got != "simple/path" {
		t.Fatalf("unexpected %q", got)
	}
	if got := ShellQuote("a b'c"); got != `'a b'\''c'` {
		t.Fatalf("unexpected %q", got)
	}
}
