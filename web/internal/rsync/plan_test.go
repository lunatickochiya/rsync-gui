package rsync

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunatickochiya/rsync-gui/web/internal/config"
)

func TestBuildPlanLocal(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dest := filepath.Join(root, "dest")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := BuildPlan(config.DefaultOptions(), []string{src}, dest)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Cleanup()
	if len(p.Transfers) != 1 {
		t.Fatalf("transfers = %+v", p.Transfers)
	}
	if p.Destination != dest+string(filepath.Separator) {
		t.Fatalf("destination = %q", p.Destination)
	}
	if p.Transfers[0].Sources[0] != src {
		t.Fatalf("source = %q", p.Transfers[0].Sources[0])
	}
	cmd := p.CommandFor(p.Transfers[0], PhaseCopy)
	if cmd[0] != "rsync" || !strings.Contains(strings.Join(cmd, " "), "-- "+src+" "+dest+"/") {
		t.Fatalf("command = %v", cmd)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("destination was not created: %v", err)
	}
}

func TestBuildPlanContentsOnly(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	opts := config.DefaultOptions()
	opts.ContentsOnly = true
	p, err := BuildPlan(opts, []string{src}, filepath.Join(root, "dest"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Cleanup()
	if got := p.Transfers[0].Sources[0]; got != src+string(filepath.Separator) {
		t.Fatalf("source = %q", got)
	}
}

func TestBuildPlanMissingSource(t *testing.T) {
	root := t.TempDir()
	_, err := BuildPlan(config.DefaultOptions(), []string{filepath.Join(root, "missing")}, filepath.Join(root, "dest"))
	if err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildPlanGroupsRemoteHosts(t *testing.T) {
	root := t.TempDir()
	sources := []string{
		"sftp://alice@nas1:2222/volume1/a",
		"sftp://alice@nas1:2222/volume1/b",
		"bob@nas2:/data/c",
	}
	p, err := BuildPlan(config.DefaultOptions(), sources, filepath.Join(root, "dest"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Cleanup()
	if len(p.Transfers) != 2 {
		t.Fatalf("transfers = %+v", p.Transfers)
	}
	if p.Transfers[0].Port != 2222 || !p.Transfers[0].Remote || len(p.Transfers[0].Sources) != 2 {
		t.Fatalf("first = %+v", p.Transfers[0])
	}
	found := map[string]bool{}
	for _, tr := range p.Transfers {
		found[tr.Description] = true
	}
	if !found["alice@nas1"] || !found["bob@nas2"] {
		t.Fatalf("descriptions = %+v", found)
	}
}

func TestBuildPlanRemoteToRemoteRejected(t *testing.T) {
	_, err := BuildPlan(config.DefaultOptions(), []string{"nas:/data"}, "sftp://other:2222/backup")
	if err == nil || !strings.Contains(err.Error(), "不支持") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildPlanDestinationInsideSource(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := BuildPlan(config.DefaultOptions(), []string{src}, filepath.Join(src, "sub"))
	if err == nil || !strings.Contains(err.Error(), "内部") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildPlanGitTracked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "untracked.tmp"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("add", "tracked.txt")
	run("commit", "-qm", "init")

	opts := config.DefaultOptions()
	opts.GitTrackedOnly = true
	opts.GitIncludeRepo = false
	p, err := BuildPlan(opts, []string{repo}, filepath.Join(root, "dest"))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.TempFiles) != 1 {
		t.Fatalf("temp files = %v", p.TempFiles)
	}
	data, err := os.ReadFile(p.TempFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("tracked.txt\x00")) || bytes.Contains(data, []byte("untracked.tmp")) {
		t.Fatalf("file list = %q", data)
	}
	listPath := p.TempFiles[0]
	p.Cleanup()
	if _, err := os.Stat(listPath); err == nil {
		t.Fatal("temp file not removed")
	}
}

func TestRemoveEmptyDirectories(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	removed, left := removeEmptyDirectories(filepath.Join(root, "a"), false)
	if removed != 3 || len(left) != 0 {
		t.Fatalf("removed=%d left=%v", removed, left)
	}
	if _, err := os.Stat(filepath.Join(root, "a")); !os.IsNotExist(err) {
		t.Fatal("source root not removed")
	}

	if err := os.MkdirAll(filepath.Join(root, "keep", "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	removed, _ = removeEmptyDirectories(filepath.Join(root, "keep"), true)
	if removed != 1 {
		t.Fatalf("removed=%d", removed)
	}
	if _, err := os.Stat(filepath.Join(root, "keep")); err != nil {
		t.Fatal("keepTop removed the top")
	}

	nonEmpty := filepath.Join(root, "nonempty")
	if err := os.MkdirAll(nonEmpty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nonEmpty, "file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, left = removeEmptyDirectories(nonEmpty, false)
	if removed != 0 || len(left) != 1 {
		t.Fatalf("removed=%d left=%v", removed, left)
	}
}

func TestRunCopyAndVerify(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dest := filepath.Join(root, "dest")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "文件.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := config.DefaultOptions()
	opts.VerifyAfter = true
	p, err := BuildPlan(opts, []string{src}, dest)
	if err != nil {
		t.Fatal(err)
	}
	sum := runJob(t, p)
	if !sum.Success() || sum.Verified != 1 || sum.Failed != 0 {
		t.Fatalf("summary = %+v", sum)
	}
	data, err := os.ReadFile(filepath.Join(dest, "src", "sub", "文件.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("dest = %q err=%v", data, err)
	}
}

func TestRunVerifiedMove(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dest := filepath.Join(root, "dest")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := config.DefaultOptions()
	opts.VerifyAfter = true
	opts.RemoveSourceFiles = true
	opts.RemoveSourceDirs = true
	p, err := BuildPlan(opts, []string{src}, dest)
	if err != nil {
		t.Fatal(err)
	}
	sum := runJob(t, p)
	if !sum.Success() || sum.SourcesRemoved != 1 || sum.Verified != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("source still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "src", "sub", "a.txt")); err != nil {
		t.Fatalf("destination missing: %v", err)
	}
}

func TestRunVerificationDifferences(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dest := filepath.Join(root, "dest")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dest, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("AAAAA"), 0o644); err != nil {
		t.Fatal(err)
	}
	destFile := filepath.Join(dest, "src", "file.txt")
	if err := os.WriteFile(destFile, []byte("BBBBB"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(destFile, future, future); err != nil {
		t.Fatal(err)
	}
	opts := config.DefaultOptions()
	opts.VerifyAfter = true
	opts.Existing = "skip-newer"
	p, err := BuildPlan(opts, []string{src}, dest)
	if err != nil {
		t.Fatal(err)
	}
	sum := runJob(t, p)
	if sum.Differences != 1 || sum.VerifyFailed != 0 || sum.Success() {
		t.Fatalf("summary = %+v", sum)
	}
	data, _ := os.ReadFile(destFile)
	if string(data) != "BBBBB" {
		t.Fatalf("destination changed: %q", data)
	}
}

func runJob(t *testing.T, p *Plan) Summary {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	events := Start(ctx, p)
	var sum Summary
	for ev := range events {
		if ev.Kind == EventDone && ev.Summary != nil {
			sum = *ev.Summary
		}
	}
	if sum.Total == 0 {
		t.Fatal("job did not finish")
	}
	return sum
}
