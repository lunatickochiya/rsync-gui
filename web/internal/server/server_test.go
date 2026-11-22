package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunatickochiya/rsync-gui/web/internal/config"
)

func newTestServer(t *testing.T, token string) (*Server, *httptest.Server) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	srv, err := New(Config{Version: "test", Listen: "127.0.0.1:0", Token: token})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func get(t *testing.T, client *http.Client, url string, header http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for key, values := range header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func postJSON(t *testing.T, client *http.Client, url string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var value T
	if err := json.NewDecoder(resp.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func noRedirectClient() *http.Client {
	return &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

func TestAuthentication(t *testing.T) {
	_, ts := newTestServer(t, "s3cret")
	client := noRedirectClient()

	resp := get(t, client, ts.URL+"/", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("index status = %d, want 401", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = get(t, client, ts.URL+"/?token=s3cret", nil)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("token login status = %d, want 302", resp.StatusCode)
	}
	cookies := resp.Cookies()
	if len(cookies) == 0 || cookies[0].Name != "rsync_web_token" {
		t.Fatalf("login did not set the auth cookie: %v", cookies)
	}
	_ = resp.Body.Close()

	header := http.Header{"Authorization": []string{"Bearer s3cret"}}
	resp = get(t, client, ts.URL+"/api/bootstrap", header)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bootstrap with bearer status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = get(t, client, ts.URL+"/api/bootstrap", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bootstrap without token status = %d, want 401", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestBootstrapPlanBrowse(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "note.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, ts := newTestServer(t, "")
	client := ts.Client()

	resp := get(t, client, ts.URL+"/api/bootstrap", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bootstrap status = %d", resp.StatusCode)
	}
	boot := decode[bootstrapResponse](t, resp)
	if boot.RsyncVersion == "" || boot.Version != "test" {
		t.Fatalf("bootstrap = %+v", boot)
	}
	if len(boot.Presets) == 0 {
		t.Fatal("no presets in bootstrap")
	}

	body := map[string]any{
		"options":     config.DefaultOptions(),
		"sources":     []string{src},
		"destination": dst,
	}
	resp = postJSON(t, client, ts.URL+"/api/plan", body)
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("plan status = %d: %s", resp.StatusCode, data)
	}
	plan := decode[planResponse](t, resp)
	if len(plan.Commands) == 0 || !strings.Contains(plan.Commands[0].Command, "rsync ") {
		t.Fatalf("plan commands = %+v", plan.Commands)
	}
	if plan.Destructive {
		t.Fatalf("default plan should not be destructive: %+v", plan)
	}

	resp = get(t, client, ts.URL+"/api/browse?path="+root, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("browse status = %d", resp.StatusCode)
	}
	browse := decode[browseResponse](t, resp)
	found := false
	for _, entry := range browse.Entries {
		if entry.Name == "src" && entry.Dir {
			found = true
		}
	}
	if !found {
		t.Fatalf("browse did not list src: %+v", browse.Entries)
	}
}

func TestDestructiveStartRequiresConfirmation(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := config.DefaultOptions()
	opts.DeleteExtraneous = true
	body := map[string]any{
		"options":     opts,
		"sources":     []string{src},
		"destination": filepath.Join(root, "dst"),
	}
	_, ts := newTestServer(t, "")
	resp := postJSON(t, ts.Client(), ts.URL+"/api/start", body)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("start status = %d, want 409", resp.StatusCode)
	}
	plan := decode[planResponse](t, resp)
	if !plan.ConfirmRequired || !plan.Destructive {
		t.Fatalf("confirmation response = %+v", plan)
	}
}

func TestStartCopiesFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("web job payload\n")
	if err := os.WriteFile(filepath.Join(src, "a.txt"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	opts := config.DefaultOptions()
	opts.Progress = "overall"
	body := map[string]any{
		"options":     opts,
		"sources":     []string{src},
		"destination": dst,
		"confirm":     true,
	}
	_, ts := newTestServer(t, "")
	client := ts.Client()
	resp := postJSON(t, client, ts.URL+"/api/start", body)
	if resp.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("start status = %d: %s", resp.StatusCode, data)
	}
	start := decode[startResponse](t, resp)
	if !start.Started {
		t.Fatalf("start = %+v", start)
	}

	deadline := time.Now().Add(8 * time.Second)
	for {
		resp = get(t, client, ts.URL+"/api/bootstrap", nil)
		boot := decode[bootstrapResponse](t, resp)
		if boot.Job.Done {
			if boot.Job.Summary == nil || boot.Job.Summary.Failed != 0 || boot.Job.Summary.OK == 0 {
				t.Fatalf("job summary = %+v", boot.Job.Summary)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job did not finish")
		}
		time.Sleep(100 * time.Millisecond)
	}

	copied, err := os.ReadFile(filepath.Join(dst, "src", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(copied, payload) {
		t.Fatalf("copied data = %q", copied)
	}
}

func TestUndoRestoresDestination(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(filepath.Join(dst, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A file that the run replaces, an extraneous file that --delete removes.
	write(filepath.Join(dst, "src", "old.txt"), "old")
	write(filepath.Join(dst, "src", "extra.txt"), "extraneous")
	write(filepath.Join(src, "old.txt"), "completely new and longer content")
	write(filepath.Join(src, "fresh.txt"), "fresh")

	opts := config.DefaultOptions()
	opts.Progress = "none"
	opts.DeleteExtraneous = true
	body := map[string]any{
		"options":     opts,
		"sources":     []string{src},
		"destination": dst,
		"confirm":     true,
	}
	_, ts := newTestServer(t, "")
	client := ts.Client()
	resp := postJSON(t, client, ts.URL+"/api/start", body)
	if resp.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("start status = %d: %s", resp.StatusCode, data)
	}

	// The run finished and can be undone.
	deadline := time.Now().Add(10 * time.Second)
	var job jobSnapshot
	for {
		resp = get(t, client, ts.URL+"/api/bootstrap", nil)
		boot := decode[bootstrapResponse](t, resp)
		job = boot.Job
		if job.Done && job.Undo != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not finish: %+v", job.Summary)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// fresh.txt is new; old.txt and extra.txt are kept in the backup dir, and so
	// is the "src" directory that mirrors their path below the destination.
	if job.Undo.Deletions != 1 || job.Undo.Restorations != 3 {
		t.Fatalf("undo info = %+v", job.Undo)
	}
	if data, _ := os.ReadFile(filepath.Join(dst, "src", "old.txt")); string(data) != "completely new and longer content" {
		t.Fatalf("old.txt after copy = %q", data)
	}

	// Without confirmation the endpoint refuses.
	resp = postJSON(t, client, ts.URL+"/api/undo", map[string]any{"confirm": false})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("undo without confirm status = %d", resp.StatusCode)
	}

	// Undo restores the destination to its state before the run.
	resp = postJSON(t, client, ts.URL+"/api/undo", map[string]any{"confirm": true})
	if resp.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("undo status = %d: %s", resp.StatusCode, data)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		resp = get(t, client, ts.URL+"/api/bootstrap", nil)
		boot := decode[bootstrapResponse](t, resp)
		job = boot.Job
		if job.Done && job.Undo != nil && job.Undo.Result != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("undo did not finish")
		}
		time.Sleep(100 * time.Millisecond)
	}
	result := job.Undo.Result
	if result.Failed != 0 || result.Deleted != 1 || result.Restored != 3 {
		t.Fatalf("undo result = %+v", result)
	}
	if data, _ := os.ReadFile(filepath.Join(dst, "src", "old.txt")); string(data) != "old" {
		t.Fatalf("old.txt after undo = %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(dst, "src", "extra.txt")); string(data) != "extraneous" {
		t.Fatalf("extra.txt after undo = %q", data)
	}
	if _, err := os.Stat(filepath.Join(dst, "src", "fresh.txt")); !os.IsNotExist(err) {
		t.Fatal("fresh.txt still exists after undo")
	}
	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".rsync-gui-undo-") {
			t.Fatalf("backup dir left behind: %s", entry.Name())
		}
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	_, ts := newTestServer(t, "")
	opts := config.DefaultOptions()
	opts.Archive = false
	opts.Recursive = true
	opts.Compress = true

	resp := postJSON(t, ts.Client(), ts.URL+"/api/settings/export", map[string]any{
		"options": opts,
		"name":    "web test",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status = %d", resp.StatusCode)
	}
	if disposition := resp.Header.Get("Content-Disposition"); !strings.Contains(disposition, "attachment") {
		t.Fatalf("Content-Disposition = %q", disposition)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if !bytes.Contains(data, []byte(config.SettingsFormat)) {
		t.Fatalf("export body = %s", data)
	}

	resp = postJSON(t, ts.Client(), ts.URL+"/api/settings/import", json.RawMessage(data))
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("import status = %d: %s", resp.StatusCode, body)
	}
	imported := decode[map[string]any](t, resp)
	if imported["name"] != "web test" {
		t.Fatalf("imported name = %v", imported["name"])
	}
}
