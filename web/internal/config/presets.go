package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lunatickochiya/rsync-gui/web/presets"
)

// Preset is one selectable set of options.
type Preset struct {
	ID      string
	Name    string
	Label   string
	Path    string
	Options Options
	Error   string
	Builtin bool
}

// PresetSearchPaths mirrors the search order of rsync-gui, plus a ./presets
// directory for running from the source tree.
func PresetSearchPaths() []string {
	var paths []string
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dataHome = filepath.Join(home, ".local", "share")
		}
	}
	if dataHome != "" {
		paths = append(paths, filepath.Join(dataHome, "rsync-gui", "presets"))
	}
	for _, dir := range filepath.SplitList(os.Getenv("XDG_DATA_DIRS")) {
		if dir != "" {
			paths = append(paths, filepath.Join(dir, "rsync-gui", "presets"))
		}
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		paths = append(paths, filepath.Join(dir, "..", "share", "rsync-gui", "presets"))
		paths = append(paths, filepath.Join(dir, "presets"))
	}
	if wd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(wd, "presets"))
	}
	return dedupeStrings(paths)
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// BuiltinPresets decodes the embedded presets.
func BuiltinPresets() []Preset {
	entries, err := presets.FS.ReadDir(".")
	if err != nil {
		return nil
	}
	var out []Preset
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := presets.FS.ReadFile(entry.Name())
		if err != nil {
			continue
		}
		p := decodePreset(entry.Name(), "", data, true)
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// AvailablePresets lists user presets first (they override built-ins), sorted by ID.
func AvailablePresets() []Preset {
	byID := make(map[string]Preset)
	for _, dir := range PresetSearchPaths() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			id := strings.TrimSuffix(entry.Name(), ".json")
			if _, exists := byID[id]; exists {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			byID[id] = decodePreset(entry.Name(), path, data, false)
		}
	}
	for _, p := range BuiltinPresets() {
		if _, exists := byID[p.ID]; !exists {
			byID[p.ID] = p
		}
	}
	out := make([]Preset, 0, len(byID))
	for _, p := range byID {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func decodePreset(fileName, path string, data []byte, builtin bool) Preset {
	id := strings.TrimSuffix(fileName, ".json")
	p := Preset{ID: id, Name: id, Path: path, Builtin: builtin, Options: DefaultOptions()}
	var root SettingsFile
	if err := json.Unmarshal(data, &root); err != nil {
		p.Error = fmt.Sprintf("JSON 无效：%v", err)
		p.Label = id
		return p
	}
	if root.Format != SettingsFormat {
		p.Error = "不是 rsync 设置文件"
		p.Label = id
		return p
	}
	if root.Name != "" {
		p.Name = root.Name
	}
	opts := DefaultOptions()
	if err := json.Unmarshal(root.Options, &opts); err != nil {
		p.Error = fmt.Sprintf("选项无效：%v", err)
		p.Label = p.Name
		return p
	}
	if opts.Progress == "" {
		opts.Progress = "per-file"
	}
	if opts.Existing == "" {
		opts.Existing = "replace"
	}
	p.Options = opts
	p.Label = opts.Label(p.Name)
	return p
}

// FindPreset locates a preset by ID or path.
func FindPreset(name string) (Preset, bool) {
	if name == "" {
		return Preset{}, false
	}
	if strings.ContainsAny(name, `/\`) {
		if info, err := os.Stat(name); err == nil && !info.IsDir() {
			data, err := os.ReadFile(name)
			if err != nil {
				return Preset{}, false
			}
			p := decodePreset(filepath.Base(name), name, data, false)
			return p, true
		}
		return Preset{}, false
	}
	for _, p := range AvailablePresets() {
		if p.ID == name || p.ID == strings.TrimSuffix(name, ".json") {
			return p, true
		}
	}
	return Preset{}, false
}

// MergePreset applies the copy options of a preset while keeping the
// user-specific program, paths, remote shell and extra arguments.
func MergePreset(base Options, preset Options) Options {
	o := preset
	o.Program = base.Program
	o.UseBwLimit = base.UseBwLimit
	o.BwLimit = base.BwLimit
	o.UseRemoteShell = base.UseRemoteShell
	o.RemoteShell = base.RemoteShell
	o.SSHKey = base.SSHKey
	o.ExcludeFile = base.ExcludeFile
	o.LinkDest = base.LinkDest
	o.ExtraArguments = base.ExtraArguments
	o.AutoModifyWindow = base.AutoModifyWindow
	o.GitTrackedOnly = base.GitTrackedOnly
	o.GitIncludeRepo = base.GitIncludeRepo
	o.NumericIDs = base.NumericIDs
	o.OneFileSystem = base.OneFileSystem
	o.Sparse = base.Sparse
	return o
}
