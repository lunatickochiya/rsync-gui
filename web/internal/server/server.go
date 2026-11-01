// Package server serves the rsync-web browser UI and its JSON API.
package server

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/lunatickochiya/rsync-gui/web/internal/config"
)

//go:embed assets
var embeddedAssets embed.FS

// Config configures one rsync-web server instance.
type Config struct {
	Version string
	Listen  string
	Token   string
}

// Server owns the HTTP routes and the remembered transfer settings.
type Server struct {
	version      string
	listen       string
	token        string
	rsyncVersion string
	assets       fs.FS
	jobs         *JobManager

	mu  sync.RWMutex
	cfg config.AppConfig
}

// New creates a server, loading the remembered configuration.
func New(cfg Config) (*Server, error) {
	assets, err := fs.Sub(embeddedAssets, "assets")
	if err != nil {
		return nil, err
	}
	appCfg := config.LoadConfig()
	if appCfg.Options.Program == "" {
		appCfg.Options.Program = "rsync"
	}
	if appCfg.Options.Progress == "" {
		appCfg.Options.Progress = "per-file"
	}
	if appCfg.Options.Existing == "" {
		appCfg.Options.Existing = "replace"
	}
	s := &Server{
		version:      cfg.Version,
		listen:       cfg.Listen,
		token:        cfg.Token,
		rsyncVersion: detectRsyncVersion(appCfg.Options.Program),
		assets:       assets,
		jobs:         NewJobManager(),
		cfg:          appCfg,
	}
	return s, nil
}

// RsyncVersion returns the detected rsync version for the startup banner.
func (s *Server) RsyncVersion() string { return s.rsyncVersion }

// Handler returns the HTTP handler with security headers and all routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /assets/", s.handleAssets)
	mux.HandleFunc("GET /favicon.ico", s.handleFavicon)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	})

	mux.HandleFunc("GET /api/bootstrap", s.auth(s.handleBootstrap))
	mux.HandleFunc("POST /api/plan", s.auth(s.handlePlan))
	mux.HandleFunc("POST /api/start", s.auth(s.handleStart))
	mux.HandleFunc("POST /api/cancel", s.auth(s.handleCancel))
	mux.HandleFunc("GET /api/events", s.auth(s.handleEvents))
	mux.HandleFunc("GET /api/browse", s.auth(s.handleBrowse))
	mux.HandleFunc("POST /api/settings/export", s.auth(s.handleSettingsExport))
	mux.HandleFunc("POST /api/settings/import", s.auth(s.handleSettingsImport))

	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy",
			"default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; "+
				"img-src 'self' data:; connect-src 'self'; base-uri 'none'; "+
				"form-action 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if s.token != "" {
		if q := r.URL.Query().Get("token"); q != "" && tokenEqual(q, s.token) {
			http.SetCookie(w, &http.Cookie{
				Name:     "rsync_web_token",
				Value:    s.token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
				Secure:   r.TLS != nil,
				MaxAge:   30 * 24 * 60 * 60,
			})
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if !s.authorized(r) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, loginPage)
			return
		}
	}
	data, err := fs.ReadFile(s.assets, "index.html")
	if err != nil {
		http.Error(w, "界面文件缺失", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/assets/favicon.svg", http.StatusTemporaryRedirect)
}

func (s *Server) handleAssets(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/assets/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.StripPrefix("/assets/", http.FileServer(http.FS(s.assets))).ServeHTTP(w, r)
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.token == "" || s.authorized(r) {
			next(w, r)
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "需要访问令牌：在页面登录，或使用 Authorization: Bearer <token>",
		})
	}
}

func (s *Server) authorized(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	if q := r.URL.Query().Get("token"); q != "" && tokenEqual(q, s.token) {
		return true
	}
	if c, err := r.Cookie("rsync_web_token"); err == nil && tokenEqual(c.Value, s.token) {
		return true
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") && tokenEqual(strings.TrimPrefix(auth, "Bearer "), s.token) {
		return true
	}
	return false
}

func tokenEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

type bootstrapResponse struct {
	Version            string           `json:"version"`
	RsyncVersion       string           `json:"rsyncVersion"`
	Platform           string           `json:"platform"`
	Listen             string           `json:"listen"`
	AuthEnabled        bool             `json:"authEnabled"`
	Options            config.Options   `json:"options"`
	Sources            []string         `json:"sources"`
	Destination        string           `json:"destination"`
	RecentDestinations []string         `json:"recentDestinations"`
	Presets            []presetInfo     `json:"presets"`
	Job                jobSnapshot      `json:"job"`
	Defaults           config.Options   `json:"defaults"`
	SettingsFormat     string           `json:"settingsFormat"`
	SettingsVersion    int              `json:"settingsVersion"`
	Features           map[string]bool  `json:"features"`
	Limits             map[string]int64 `json:"limits"`
}

type presetInfo struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Label   string         `json:"label"`
	Builtin bool           `json:"builtin"`
	Error   string         `json:"error,omitempty"`
	Options config.Options `json:"options"`
}

func (s *Server) handleBootstrap(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	presets := config.AvailablePresets()
	items := make([]presetInfo, 0, len(presets))
	for _, p := range presets {
		items = append(items, presetInfo{
			ID: p.ID, Name: p.Name, Label: p.Label,
			Builtin: p.Builtin, Error: p.Error, Options: p.Options,
		})
	}
	writeJSON(w, http.StatusOK, bootstrapResponse{
		Version:            s.version,
		RsyncVersion:       s.rsyncVersion,
		Platform:           runtime.GOOS + "/" + runtime.GOARCH,
		Listen:             s.listen,
		AuthEnabled:        s.token != "",
		Options:            cfg.Options,
		Sources:            append([]string(nil), cfg.Sources...),
		Destination:        cfg.Destination,
		RecentDestinations: append([]string(nil), cfg.RecentDestinations...),
		Presets:            items,
		Job:                s.jobs.Snapshot(),
		Defaults:           config.DefaultOptions(),
		SettingsFormat:     config.SettingsFormat,
		SettingsVersion:    config.SettingsVersion,
		Features: map[string]bool{
			"verify": true, "move": true, "browse": true,
			"settings": true, "git": true,
		},
		Limits: map[string]int64{"bodyBytes": 2 << 20, "logs": maxJobLogs},
	})
}

func (s *Server) currentOptions() config.Options {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Options.Clone()
}

func (s *Server) remember(opts config.Options, sources []string, dest string) error {
	s.mu.Lock()
	s.cfg.Options = opts
	s.cfg.Sources = append([]string(nil), sources...)
	s.cfg.RememberDestination(dest)
	cfg := s.cfg
	s.mu.Unlock()
	return cfg.Save()
}

func detectRsyncVersion(program string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, program, "--version").Output()
	if err != nil {
		return "未找到 rsync"
	}
	line := strings.SplitN(string(out), "\n", 2)[0]
	if strings.TrimSpace(line) == "" {
		return "rsync"
	}
	re := regexp.MustCompile(`rsync\s+version\s+([0-9.]+)`)
	if m := re.FindStringSubmatch(line); m != nil {
		return "rsync " + m[1]
	}
	return strings.TrimSpace(line)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(value)
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("请求 JSON 无效：%w", err)
	}
	return nil
}

const maxJobLogs = 3000
