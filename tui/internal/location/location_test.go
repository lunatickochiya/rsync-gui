package location

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLocal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	loc, err := Parse("~/photos")
	if err != nil {
		t.Fatal(err)
	}
	if loc.Kind != Local || loc.Path != filepath.Join(home, "photos") {
		t.Fatalf("loc = %+v", loc)
	}
	if loc.StoreSpec() != filepath.Join(home, "photos") {
		t.Fatalf("store = %q", loc.StoreSpec())
	}

	loc, err = Parse("file:///tmp/a%20b")
	if err != nil {
		t.Fatal(err)
	}
	if loc.Path != "/tmp/a b" {
		t.Fatalf("file url = %q", loc.Path)
	}
}

func TestParseRsyncRemote(t *testing.T) {
	loc, err := Parse("user@nas:/volume1/photos")
	if err != nil {
		t.Fatal(err)
	}
	if loc.Kind != Remote || loc.User != "user" || loc.Host != "nas" || loc.Path != "/volume1/photos" {
		t.Fatalf("loc = %+v", loc)
	}
	if loc.RsyncSpec() != "user@nas:/volume1/photos" {
		t.Fatalf("spec = %q", loc.RsyncSpec())
	}
}

func TestParseSftpPreservesPort(t *testing.T) {
	loc, err := Parse("sftp://alice@nas:2222/volume1/photos")
	if err != nil {
		t.Fatal(err)
	}
	if loc.Port != 2222 || loc.User != "alice" || loc.Host != "nas" || loc.Path != "/volume1/photos" {
		t.Fatalf("loc = %+v", loc)
	}
	if loc.RsyncSpec() != "alice@nas:/volume1/photos" {
		t.Fatalf("spec = %q", loc.RsyncSpec())
	}
	if loc.StoreSpec() != "sftp://alice@nas:2222/volume1/photos" {
		t.Fatalf("store = %q", loc.StoreSpec())
	}
}

func TestParseIPv6Remote(t *testing.T) {
	loc, err := Parse("[::1]:/data")
	if err != nil {
		t.Fatal(err)
	}
	if loc.Kind != Remote || loc.Host != "::1" || loc.Path != "/data" {
		t.Fatalf("loc = %+v", loc)
	}
	if loc.RsyncSpec() != "[::1]:/data" {
		t.Fatalf("spec = %q", loc.RsyncSpec())
	}
}

func TestLocalColon(t *testing.T) {
	loc, err := Parse("/tmp/a:b")
	if err != nil {
		t.Fatal(err)
	}
	if loc.Kind != Local {
		t.Fatalf("expected local: %+v", loc)
	}
	// Without a leading slash this is rsync's remote syntax.
	loc, err = Parse("dir:name")
	if err != nil {
		t.Fatal(err)
	}
	if loc.Kind != Remote || loc.Host != "dir" || loc.Path != "name" {
		t.Fatalf("expected remote: %+v", loc)
	}
}

func TestArgForContentsOnly(t *testing.T) {
	dir := t.TempDir()
	loc, err := Parse(dir)
	if err != nil {
		t.Fatal(err)
	}
	arg := ArgFor(loc, true)
	if arg != dir+string(filepath.Separator) {
		t.Fatalf("arg = %q", arg)
	}
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	loc, _ = Parse(file)
	if ArgFor(loc, true) != file {
		t.Fatalf("files must not get a trailing slash")
	}
}
