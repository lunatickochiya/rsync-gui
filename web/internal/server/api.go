package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lunatickochiya/rsync-gui/web/internal/config"
	"github.com/lunatickochiya/rsync-gui/web/internal/location"
	"github.com/lunatickochiya/rsync-gui/web/internal/rsync"
)

type planRequest struct {
	Options     json.RawMessage `json:"options"`
	Sources     []string        `json:"sources"`
	Destination string          `json:"destination"`
	Confirm     bool            `json:"confirm"`
}

type planResponse struct {
	Commands        []commandInfo `json:"commands"`
	Warnings        []string      `json:"warnings"`
	Destructive     bool          `json:"destructive"`
	Delete          bool          `json:"delete"`
	Move            bool          `json:"move"`
	Verify          bool          `json:"verify"`
	Remote          bool          `json:"remote"`
	Destination     string        `json:"destination"`
	ConfirmRequired bool          `json:"confirmRequired,omitempty"`
}

type startResponse struct {
	Started  bool          `json:"started"`
	Commands []commandInfo `json:"commands"`
	Job      jobSnapshot   `json:"job"`
}

func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	var req planRequest
	if err := readJSON(w, r, &req, 2<<20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	resp, err := s.preview(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) preview(req planRequest) (*planResponse, error) {
	opts, sources, destination, err := s.sessionInput(req)
	if err != nil {
		return nil, err
	}
	plan, err := rsync.PreviewPlan(opts, sources, destination)
	if err != nil {
		return nil, err
	}
	defer plan.Cleanup()
	return planResponseFor(plan), nil
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	var req planRequest
	if err := readJSON(w, r, &req, 2<<20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if s.jobs.Running() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "已有传输正在进行，请先等待或取消"})
		return
	}

	opts, sources, destination, err := s.sessionInput(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	previewPlan, err := rsync.PreviewPlan(opts, sources, destination)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	preview := planResponseFor(previewPlan)
	previewPlan.Cleanup()

	if preview.Destructive && !req.Confirm {
		preview.ConfirmRequired = true
		writeJSON(w, http.StatusConflict, preview)
		return
	}

	plan, err := rsync.BuildPlan(opts, sources, destination)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	commands := commandsFor(plan)
	if !s.jobs.Start(plan, commands) {
		plan.Cleanup()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "已有传输正在进行"})
		return
	}
	_ = s.remember(plan.Options, sources, destination)
	writeJSON(w, http.StatusAccepted, startResponse{
		Started:  true,
		Commands: commands,
		Job:      s.jobs.Snapshot(),
	})
}

func (s *Server) handleCancel(w http.ResponseWriter, _ *http.Request) {
	if !s.jobs.Cancel() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "没有正在运行的传输"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"cancelling": true})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	s.jobs.ServeSSE(w, r)
}

func (s *Server) sessionInput(req planRequest) (config.Options, []string, string, error) {
	opts, err := s.optionsFromRaw(req.Options)
	if err != nil {
		return opts, nil, "", err
	}

	s.mu.RLock()
	rememberedSources := append([]string(nil), s.cfg.Sources...)
	rememberedDest := s.cfg.Destination
	s.mu.RUnlock()

	sources := req.Sources
	if sources == nil {
		sources = rememberedSources
	}
	destination := strings.TrimSpace(req.Destination)
	if destination == "" && req.Sources == nil {
		destination = rememberedDest
	}
	if len(sources) == 0 {
		return opts, sources, destination, fmt.Errorf("请先添加至少一个源")
	}
	if destination == "" {
		return opts, sources, destination, fmt.Errorf("请选择目标目录")
	}
	return opts, sources, destination, nil
}

func (s *Server) optionsFromRaw(raw json.RawMessage) (config.Options, error) {
	opts := s.currentOptions()
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &opts); err != nil {
			return opts, fmt.Errorf("选项 JSON 无效：%w", err)
		}
	}
	if strings.TrimSpace(opts.Program) == "" {
		opts.Program = "rsync"
	}
	if opts.Progress == "" {
		opts.Progress = "per-file"
	}
	if opts.Existing == "" {
		opts.Existing = "replace"
	}
	return opts, nil
}

func planResponseFor(plan *rsync.Plan) *planResponse {
	o := plan.Options
	resp := &planResponse{
		Commands:    commandsFor(plan),
		Warnings:    warningsFor(plan),
		Destructive: !o.DryRun && (o.DeleteExtraneous || o.RemoveSourceFiles),
		Delete:      o.DeleteExtraneous,
		Move:        o.RemoveSourceFiles,
		Verify:      o.CanVerify(),
		Remote:      plan.DestinationRemote,
		Destination: plan.Destination,
	}
	for _, tr := range plan.Transfers {
		if tr.Remote {
			resp.Remote = true
		}
	}
	return resp
}

func warningsFor(plan *rsync.Plan) []string {
	o := plan.Options
	out := make([]string, 0)
	if o.DryRun {
		out = append(out, "试运行已开启：rsync 只列出将要发生的操作，不会修改文件。")
	}
	if o.DeleteExtraneous {
		out = append(out, "将删除目标目录中源里不存在的文件（--delete）。")
	}
	if o.RemoveSourceFiles {
		if o.VerifiedMove() {
			out = append(out, "先复制并校验，校验通过后才删除源文件。")
		} else {
			out = append(out, "传输完成后将删除源文件（--remove-source-files）。")
		}
		if o.RemoveSourceDirs {
			out = append(out, "传输后会尝试删除源中变空的目录（只使用 rmdir）。")
		}
	}
	if o.GitTrackedOnly && o.ContentsOnly {
		out = append(out, "“仅 Git 跟踪文件”模式下会忽略“只复制目录内容”。")
	}
	if o.VerifyAfter && o.DryRun {
		out = append(out, "试运行下会跳过复制后校验。")
	}
	remote := plan.DestinationRemote
	for _, tr := range plan.Transfers {
		if tr.Remote {
			remote = true
			break
		}
	}
	if remote {
		out = append(out, "远程传输只使用 SSH 密钥或 ssh-agent；服务端不会处理密码和未知主机密钥交互。")
	}
	return out
}

func commandsFor(plan *rsync.Plan) []commandInfo {
	phases := []rsync.Phase{rsync.PhaseCopy}
	if plan.Options.CanVerify() {
		phases = append(phases, rsync.PhaseVerify)
	}
	if plan.Options.VerifiedMove() {
		phases = append(phases, rsync.PhaseRemove)
	}
	out := make([]commandInfo, 0)
	for _, tr := range plan.Transfers {
		for _, phase := range phases {
			cmd := plan.CommandFor(tr, phase)
			if len(cmd) == 0 {
				continue
			}
			out = append(out, commandInfo{
				Phase:   phase.Label(),
				Command: config.JoinCommand(cmd[0], cmd[1:]),
			})
		}
	}
	return out
}

type browseEntry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Dir     bool      `json:"dir"`
	Symlink bool      `json:"symlink"`
	Size    int64     `json:"size,omitempty"`
	ModTime time.Time `json:"modTime"`
}

type browseResponse struct {
	Path      string        `json:"path"`
	Parent    string        `json:"parent,omitempty"`
	Home      string        `json:"home"`
	Entries   []browseEntry `json:"entries"`
	Truncated bool          `json:"truncated"`
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("path"))
	if raw == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		raw = home
	}
	loc, err := location.Parse(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if loc.Kind != location.Local {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "网页文件浏览器只能浏览运行服务的本机路径"})
		return
	}
	path := loc.Path
	info, err := os.Stat(path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("无法读取 %q：%v", path, err)})
		return
	}
	if !info.IsDir() {
		path = filepath.Dir(path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": fmt.Sprintf("无法列出 %q：%v", path, err)})
		return
	}

	const limit = 2000
	truncated := false
	if len(entries) > limit {
		entries = entries[:limit]
		truncated = true
	}
	out := make([]browseEntry, 0, len(entries))
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name())
		fi, statErr := os.Stat(child)
		if statErr != nil {
			fi, statErr = entry.Info()
			if statErr != nil {
				continue
			}
		}
		out = append(out, browseEntry{
			Name:    entry.Name(),
			Path:    child,
			Dir:     fi.IsDir(),
			Symlink: entry.Type()&os.ModeSymlink != 0,
			Size:    fi.Size(),
			ModTime: fi.ModTime(),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})

	parent := filepath.Dir(path)
	if parent == path {
		parent = ""
	}
	home, _ := os.UserHomeDir()
	writeJSON(w, http.StatusOK, browseResponse{
		Path: path, Parent: parent, Home: home,
		Entries: out, Truncated: truncated,
	})
}

type settingsRequest struct {
	Options json.RawMessage `json:"options"`
	Name    string          `json:"name"`
}

func (s *Server) handleSettingsExport(w http.ResponseWriter, r *http.Request) {
	var req settingsRequest
	if err := readJSON(w, r, &req, 2<<20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	opts, err := s.optionsFromRaw(req.Options)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "rsync-web"
	}
	data, err := config.SettingsJSON(name, opts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="rsync-web-settings.json"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (s *Server) handleSettingsImport(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "设置文件过大或读取失败"})
		return
	}
	opts, name, err := config.ParseSettings(data, s.currentOptions())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"options": opts,
		"name":    name,
	})
}
