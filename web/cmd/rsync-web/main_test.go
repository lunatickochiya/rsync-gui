package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccessLinesWSL(t *testing.T) {
	lines := accessLines("0.0.0.0", "8787", "token", true)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"http://127.0.0.1:8787/?token=token",
		"http://localhost:8787/?token=token",
		"WSL 网卡",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("WSL access lines missing %q:\n%s", want, joined)
		}
	}
}

func TestAccessLinesNonWSL(t *testing.T) {
	lines := accessLines("0.0.0.0", "8787", "", false)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "localhost:8787") {
		t.Fatalf("non-WSL access lines should not advertise localhost:\n%s", joined)
	}
	if !strings.Contains(joined, "http://127.0.0.1:8787/") {
		t.Fatalf("non-WSL access lines missing loopback URL:\n%s", joined)
	}
}

func TestWithEnvReplacesExisting(t *testing.T) {
	env := withEnv([]string{"A=1", "X=old", "B=2"}, "X", "new")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "X=old") || !strings.Contains(joined, "X=new") {
		t.Fatalf("withEnv() = %v", env)
	}
	if !strings.Contains(joined, "A=1") || !strings.Contains(joined, "B=2") {
		t.Fatalf("withEnv() dropped unrelated values: %v", env)
	}
}

func TestDefaultBackgroundPaths(t *testing.T) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := defaultPIDFile("8787"), filepath.Join(configDir, "rsync-web", "rsync-web-8787.pid"); got != want {
		t.Fatalf("defaultPIDFile() = %q, want %q", got, want)
	}
	if got, want := defaultLogFile("8787"), filepath.Join(cacheDir, "rsync-web", "rsync-web-8787.log"); got != want {
		t.Fatalf("defaultLogFile() = %q, want %q", got, want)
	}
}

func TestIsLoopback(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "::1", "localhost"} {
		if !isLoopback(host) {
			t.Fatalf("%q should be loopback", host)
		}
	}
	for _, host := range []string{"0.0.0.0", "192.168.1.2", ""} {
		if isLoopback(host) {
			t.Fatalf("%q should not be loopback", host)
		}
	}
}
