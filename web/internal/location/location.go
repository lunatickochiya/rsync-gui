// Package location parses the local and remote source/destination notations
// supported by rsync-gui: local paths, file:// URLs, sftp:// URLs and
// [user@]host:path.
package location

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Kind is the location type.
type Kind int

const (
	Local Kind = iota
	Remote
)

// Location is a parsed source or destination.
type Location struct {
	Raw    string
	Kind   Kind
	Path   string // absolute local path, or path on the remote host
	User   string
	Host   string
	Port   int
	Scheme string
}

func (l Location) IsRemote() bool { return l.Kind == Remote }

// RsyncSpec renders the value passed to rsync.
func (l Location) RsyncSpec() string {
	if l.Kind == Local {
		return l.Path
	}
	host := l.Host
	if l.User != "" {
		host = l.User + "@" + host
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return host + ":" + l.Path
}

// StoreSpec returns the string remembered in the session: the absolute local
// path, or the original remote notation so that sftp:// ports are preserved.
func (l Location) StoreSpec() string {
	if l.Kind == Local {
		return l.Path
	}
	return l.Raw
}

// Display renders a compact user-facing path.
func (l Location) Display() string {
	if l.Kind == Local {
		return DisplayPath(l.Path)
	}
	return l.Raw
}

// DisplayPath shortens the home directory to ~.
func DisplayPath(path string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if path == home {
			return "~"
		}
		if strings.HasPrefix(path, home+string(filepath.Separator)) {
			return "~" + path[len(home):]
		}
	}
	return path
}

// Parse parses one source or destination string.
func Parse(spec string) (Location, error) {
	raw := strings.TrimSpace(spec)
	if raw == "" {
		return Location{}, fmt.Errorf("路径为空")
	}
	lower := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(lower, "sftp://"), strings.HasPrefix(lower, "ssh://"):
		return parseURL(raw)
	case strings.HasPrefix(lower, "file://"):
		u, err := url.Parse(raw)
		if err != nil {
			return Location{}, fmt.Errorf("无效的 file:// 地址：%w", err)
		}
		if u.Host != "" && u.Host != "localhost" {
			return Location{}, fmt.Errorf("不支持远程 file:// 主机 %q", u.Host)
		}
		path := u.Path
		if path == "" {
			path = u.Opaque
		}
		return parseLocal(raw, path)
	case isRsyncRemote(raw):
		return parseRsyncRemote(raw)
	default:
		return parseLocal(raw, raw)
	}
}

func parseURL(raw string) (Location, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Location{}, fmt.Errorf("无效的远程地址：%w", err)
	}
	if u.Hostname() == "" {
		return Location{}, fmt.Errorf("远程地址缺少主机名")
	}
	port := 0
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 || n > 65535 {
			return Location{}, fmt.Errorf("无效的端口 %q", p)
		}
		port = n
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return Location{}, fmt.Errorf("远程地址不能包含查询参数或片段")
	}
	return Location{
		Raw: raw, Kind: Remote, Path: path,
		User: u.User.Username(), Host: u.Hostname(), Port: port, Scheme: u.Scheme,
	}, nil
}

var hostPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// isRsyncRemote returns true for [user@]host:path, but not for a local
// path that merely contains a colon after a slash.
func isRsyncRemote(spec string) bool {
	if strings.HasPrefix(spec, "/") || strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "~") {
		return false
	}
	if strings.HasPrefix(spec, "[") {
		end := strings.Index(spec, "]")
		return end > 0 && end+1 < len(spec) && spec[end+1] == ':'
	}
	colon := strings.Index(spec, ":")
	if colon <= 0 {
		return false
	}
	if slash := strings.IndexAny(spec, `/\`); slash >= 0 && slash < colon {
		return false
	}
	host := spec[:colon]
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	if host == "" || !hostPattern.MatchString(host) {
		return false
	}
	return colon+1 < len(spec)
}

func parseRsyncRemote(raw string) (Location, error) {
	var hostPart, path string
	if strings.HasPrefix(raw, "[") {
		end := strings.Index(raw, "]")
		if end < 0 {
			return Location{}, fmt.Errorf("无效的 IPv6 地址")
		}
		hostPart = raw[1:end]
		if end+1 >= len(raw) || raw[end+1] != ':' {
			return Location{}, fmt.Errorf("远程地址缺少冒号")
		}
		path = raw[end+2:]
	} else {
		colon := strings.Index(raw, ":")
		hostPart = raw[:colon]
		path = raw[colon+1:]
	}
	user := ""
	if at := strings.LastIndex(hostPart, "@"); at >= 0 {
		user = hostPart[:at]
		hostPart = hostPart[at+1:]
	}
	if hostPart == "" {
		return Location{}, fmt.Errorf("远程地址缺少主机名")
	}
	if path == "" {
		path = "/"
	}
	return Location{Raw: raw, Kind: Remote, Path: path, User: user, Host: hostPart, Scheme: "ssh"}, nil
}

func parseLocal(raw, path string) (Location, error) {
	path = expandHome(path)
	if path == "" {
		return Location{}, fmt.Errorf("路径为空")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Location{}, fmt.Errorf("无法解析路径 %q：%w", path, err)
	}
	return Location{Raw: raw, Kind: Local, Path: filepath.Clean(abs)}, nil
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// ArgFor returns the rsync argument for a source. With contentsOnly, local
// directories get a trailing slash so that their contents are copied.
func ArgFor(loc Location, contentsOnly bool) string {
	if loc.Kind == Local && contentsOnly {
		if info, err := os.Stat(loc.Path); err == nil && info.IsDir() {
			if strings.HasSuffix(loc.Path, string(filepath.Separator)) {
				return loc.Path
			}
			return loc.Path + string(filepath.Separator)
		}
	}
	return loc.RsyncSpec()
}
