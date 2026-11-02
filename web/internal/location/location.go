// Package location parses the local and remote source/destination notations
// supported by rsync-gui: local paths, file:// URLs, sftp:// URLs and
// [user@]host:path.
package location

import (
	"fmt"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
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
	raw := stripQuotes(spec)
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
	// Dolphin writes "sftp://host/~/dir" for paths relative to the home
	// directory; rsync itself expects "host:dir" for that.
	path := u.Path
	switch {
	case strings.HasPrefix(path, "/~/"):
		path = path[3:]
	case path == "/~":
		path = ""
	case path == "":
		path = "/"
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return Location{}, fmt.Errorf("远程地址不能包含查询参数或片段")
	}
	return Location{
		Raw: raw, Kind: Remote, Path: cleanRemotePath(path),
		User: u.User.Username(), Host: u.Hostname(), Port: port, Scheme: u.Scheme,
	}, nil
}

// stripQuotes removes one matching pair of surrounding quotes, like the
// desktop application does for pasted paths.
func stripQuotes(text string) string {
	s := strings.TrimSpace(text)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			s = s[1 : len(s)-1]
		}
	}
	return s
}

// cleanRemotePath normalises a remote POSIX path and keeps a trailing slash,
// because rsync treats "dir/" as the contents of the directory. An empty path
// means the home directory and is rendered as "." (the desktop application
// does the same).
func cleanRemotePath(path string) string {
	if path == "" {
		return "."
	}
	trailingSlash := len(path) > 1 && strings.HasSuffix(path, "/")
	cleaned := pathpkg.Clean(path)
	if trailingSlash && cleaned != "/" {
		cleaned += "/"
	}
	return cleaned
}

// isRsyncRemote returns true for [user@]host:path, but not for a local
// path that merely contains a colon after a slash. The rules match the
// desktop application's parser: the host must not contain '/', ':' or a
// second '@', an empty path means the home directory, and "host::module"
// (rsync daemon syntax) is not a remote location here.
func isRsyncRemote(spec string) bool {
	if strings.HasPrefix(spec, "/") || strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "~") {
		return false
	}
	if strings.HasPrefix(spec, "[") {
		end := strings.Index(spec, "]")
		if end <= 0 || end+1 >= len(spec) || spec[end+1] != ':' {
			return false
		}
		return end+2 >= len(spec) || spec[end+2] != ':'
	}
	colon := strings.Index(spec, ":")
	if colon <= 0 {
		return false
	}
	// "host::module" is rsync daemon syntax, which this application does not support.
	if colon+1 < len(spec) && spec[colon+1] == ':' {
		return false
	}
	hostPart := spec[:colon]
	if strings.Contains(hostPart, "/") {
		return false
	}
	if at := strings.Index(hostPart, "@"); at >= 0 {
		if at == 0 || strings.Contains(hostPart[at+1:], "@") {
			return false
		}
		hostPart = hostPart[at+1:]
	}
	return hostPart != "" && !strings.Contains(hostPart, "@")
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
	return Location{Raw: raw, Kind: Remote, Path: cleanRemotePath(path), User: user, Host: hostPart, Scheme: "ssh"}, nil
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
