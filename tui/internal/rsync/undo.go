package rsync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lunatickochiya/rsync-gui/tui/internal/location"
)

// UndoRecord describes what one copy run changed in a local destination, so that
// the run can be undone (see Options.UndoDirName): files that it overwrote or
// deleted were moved into the backup directory inside the destination (mirroring
// their relative paths), and NewEntries lists the entries that it created.
// It is stored inside that directory, so a later session can still undo the run.
type UndoRecord struct {
	Destination string   `json:"destination"` // absolute, without a trailing slash
	UndoDirName string   `json:"undoDirName"`
	NewEntries  []string `json:"newEntries"` // relative to the destination
	FinishedAt  string   `json:"finishedAt,omitempty"`
}

// UndoRecordFileName is the name of the record file inside the backup dir.
const UndoRecordFileName = "rsync-gui-undo.json"

func (r UndoRecord) Valid() bool {
	return r.Destination != "" && r.UndoDirName != ""
}

// UndoDir is the backup directory, inside the destination.
func (r UndoRecord) UndoDir() string {
	if !r.Valid() {
		return ""
	}
	return filepath.Join(r.Destination, r.UndoDirName)
}

// RecordPath is the file that stores this record inside the backup dir.
func (r UndoRecord) RecordPath() string {
	dir := r.UndoDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, UndoRecordFileName)
}

// ParseItemizedLine parses one line of rsync --itemize-changes output of the copy
// pass: "YXcstpoguax name" or "*deleting name". The name is relative to the
// destination, without a trailing slash and without the link target that %L
// appends (" => ..." for symlinks, " -> ..." for hard links).
func ParseItemizedLine(line string) (typ byte, flags, name string, ok bool) {
	if strings.HasPrefix(line, "*deleting") {
		rest := strings.TrimLeft(line[len("*deleting"):], " ")
		if rest == "" {
			return 0, "", "", false
		}
		for len(rest) > 1 && strings.HasSuffix(rest, "/") {
			rest = rest[:len(rest)-1]
		}
		return '*', "", rest, true
	}
	if len(line) < 13 || line[11] != ' ' {
		return 0, "", "", false
	}
	typ = line[0]
	switch typ {
	case '<', '>', 'c', 'h', '.':
	default:
		return 0, "", "", false
	}
	switch line[1] {
	case 'f', 'd', 'L', 'D', 'S':
	default:
		return 0, "", "", false
	}
	flags = line[2:11]
	if strings.Contains(flags, " ") {
		return 0, "", "", false
	}
	name = line[12:]
	if idx := strings.Index(name, " => "); idx >= 0 {
		name = name[:idx]
	} else if idx := strings.Index(name, " -> "); idx >= 0 {
		name = name[:idx]
	}
	for len(name) > 1 && strings.HasSuffix(name, "/") {
		name = name[:len(name)-1]
	}
	if name == "" {
		return 0, "", "", false
	}
	return typ, flags, name, true
}

// safeRelativeName reports whether a name recorded by rsync can be joined onto
// the destination without escaping it.
func safeRelativeName(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || name == ".." {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == ".." {
			return false
		}
	}
	return true
}

func lstat(path string) (os.FileInfo, bool) {
	info, err := os.Lstat(path)
	return info, err == nil
}

func isRealDir(path string) bool {
	info, ok := lstat(path)
	return ok && info.IsDir()
}

// countTree counts the entries of a tree (directories and files), without
// following symlinks. Returns 0 for a missing path.
func countTree(path string) int {
	info, ok := lstat(path)
	if !ok {
		return 0
	}
	if !info.IsDir() {
		return 1
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return 1
	}
	count := 1
	for _, entry := range entries {
		count += countTree(filepath.Join(path, entry.Name()))
	}
	return count
}

// listDir returns the entries of a directory, sorted with deeper paths first and
// a directory after the files inside it (by name, for a stable order).
func listDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	sort.Strings(out)
	return out
}

// UndoCounts says what UndoExecute would do: entries that exist in the
// destination (to delete) and entries in the backup dir (old versions to move
// back, without the dir itself and without the record file).
func UndoCounts(rec UndoRecord) (deletions, restorations int, backupDirExists bool) {
	if !rec.Valid() {
		return 0, 0, false
	}
	backup := rec.UndoDir()
	if _, ok := lstat(backup); !ok {
		return 0, 0, false
	}
	backupDirExists = true
	restorations = countTree(backup) - 1
	if _, hasRecord := lstat(filepath.Join(backup, UndoRecordFileName)); hasRecord {
		restorations--
	}
	for _, name := range rec.NewEntries {
		if !safeRelativeName(name) {
			continue
		}
		if _, exists := lstat(filepath.Join(rec.Destination, name)); exists {
			deletions++
		}
	}
	return deletions, restorations, backupDirExists
}

// UndoResult reports what an undo did.
type UndoResult struct {
	Deleted         int      // entries created by the run that were removed
	Restored        int      // old versions that were moved back
	Failed          int      // entries that could not be deleted or restored
	Problems        []string // one line per error, in the order they happened
	LeftInBackupDir string   // old versions that could not be restored, still in this dir
	Cancelled       bool
}

func (r UndoResult) OK() bool { return !r.Cancelled && r.Failed == 0 }

// removeTree removes a tree without following symlinks or entering other file
// systems (device guard, where the platform knows devices). `removed` counts the
// entries that went away.
func removeTree(path string, device uint64, removed *int, problems *[]string, ctx context.Context, cancelled *bool) bool {
	if ctx.Err() != nil {
		*cancelled = true
		return false
	}
	info, ok := lstat(path)
	if !ok {
		return true // already gone
	}
	if dev, known := deviceOf(info); known && dev != device {
		*problems = append(*problems, "未删除（位于另一个文件系统）："+displayPath(path))
		return false
	}
	if info.IsDir() {
		for _, name := range listDir(path) {
			if !removeTree(filepath.Join(path, name), device, removed, problems, ctx, cancelled) {
				return false
			}
		}
		if err := os.Remove(path); err != nil {
			*problems = append(*problems, fmt.Sprintf("无法删除目录 %s：%v", displayPath(path), err))
			return false
		}
		*removed++
		return true
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return true
		}
		*problems = append(*problems, fmt.Sprintf("无法删除 %s：%v", displayPath(path), err))
		return false
	}
	*removed++
	return true
}

// makeDirs creates the directories of `path` (mkdir -p); a file that blocks the
// path is removed - it can only be one that the run being undone created.
func makeDirs(path string, problems *[]string) bool {
	if path == "" {
		return true
	}
	var parts []string
	dir := path
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		parts = append([]string{dir}, parts...)
		dir = parent
		if len(parts) > 128 {
			return false
		}
	}
	for _, part := range parts {
		info, ok := lstat(part)
		if ok && info.IsDir() {
			continue
		}
		if ok {
			if err := os.Remove(part); err != nil {
				*problems = append(*problems, "无法还原："+displayPath(part)+" 挡住了位置。")
				return false
			}
		}
		if err := os.Mkdir(part, 0o755); err != nil && !os.IsExist(err) {
			*problems = append(*problems, fmt.Sprintf("无法创建目录 %s：%v", displayPath(part), err))
			return false
		}
	}
	return true
}

// UndoExecute restores the destination to its state before the copy: it deletes
// the entries that the run created (deepest paths first, without following
// symlinks or entering other file systems) and moves the backed-up old versions
// back (a whole deleted tree with one rename). Between entries ctx is checked;
// `progress` (optional) is called with a permille value and a status line.
func UndoExecute(ctx context.Context, rec UndoRecord, progress func(permille int, status string)) UndoResult {
	var result UndoResult
	if !rec.Valid() {
		result.Failed++
		result.Problems = append(result.Problems, "没有可撤回的复制。")
		return result
	}
	report := func(permille int, status string) {
		if progress != nil {
			progress(permille, status)
		}
	}

	deletions, restorations, _ := UndoCounts(rec)
	total := deletions + restorations
	if total < 1 {
		total = 1
	}
	done := 0
	cancelled := false

	// --- Delete the entries that the run created. ---
	if destInfo, ok := lstat(rec.Destination); ok {
		device, _ := deviceOf(destInfo)
		entries := append([]string(nil), rec.NewEntries...)
		sort.SliceStable(entries, func(i, j int) bool {
			return strings.Count(entries[j], "/") > strings.Count(entries[i], "/")
		})
		for _, name := range entries {
			if ctx.Err() != nil {
				result.Cancelled = true
				break
			}
			if !safeRelativeName(name) {
				result.Failed++
				result.Problems = append(result.Problems, "已跳过（不是目标之下的名称）："+DisplayRelPath(name))
				continue
			}
			path := filepath.Join(rec.Destination, name)
			if _, exists := lstat(path); !exists {
				continue // never created, e.g. the transfer stopped early
			}
			report(permille(done, total), "正在删除 "+DisplayRelPath(name))
			removed := 0
			if !removeTree(path, device, &removed, &result.Problems, ctx, &cancelled) {
				result.Failed++
			} else {
				result.Deleted += removed
			}
			done++
			if cancelled {
				result.Cancelled = true
				break
			}
		}
	}

	// --- Move the backed-up old versions back. ---
	backup := rec.UndoDir()
	if !result.Cancelled && isRealDir(backup) {
		// restore moves `from` (inside the backup dir) to `to` (inside the
		// destination); it returns false when the entry or something below it
		// could not be restored.
		var restore func(from, to string, root bool) bool
		restore = func(from, to string, root bool) bool {
			if ctx.Err() != nil {
				result.Cancelled = true
				return false
			}
			fromInfo, ok := lstat(from)
			if !ok {
				return true // already restored
			}
			toInfo, toExists := lstat(to)
			countAs := 1
			switch {
			case !fromInfo.IsDir():
				// A file or symlink: whatever the run left there is replaced.
				if toExists && toInfo.IsDir() {
					removed := 0
					if dev, known := deviceOf(toInfo); known {
						if !removeTree(to, dev, &removed, &result.Problems, ctx, &cancelled) {
							return false
						}
					} else if !removeTree(to, 0, &removed, &result.Problems, ctx, &cancelled) {
						return false
					}
				} else if toExists {
					if err := os.Remove(to); err != nil {
						result.Failed++
						result.Problems = append(result.Problems, fmt.Sprintf("无法替换 %s：%v", displayPath(to), err))
						return false
					}
				}
				if !makeDirs(filepath.Dir(to), &result.Problems) {
					return false
				}
			case !toExists:
				// The whole tree with one rename.
				if !makeDirs(filepath.Dir(to), &result.Problems) {
					return false
				}
				countAs = countTree(from)
			case !toInfo.IsDir():
				// A file that the run created where the old directory was.
				if err := os.Remove(to); err != nil {
					result.Failed++
					result.Problems = append(result.Problems, fmt.Sprintf("无法还原目录 %s：%v", displayPath(to), err))
					return false
				}
			default:
				// Merge: the entries are moved back one by one, so that entries
				// that the run put there are replaced.
				for _, name := range listDir(from) {
					if root && name == UndoRecordFileName {
						continue // the record of this undo, not a backed-up file
					}
					restore(filepath.Join(from, name), filepath.Join(to, name), false)
					done++
					report(permille(done, total), "正在还原 "+DisplayRelPath(name))
					if result.Cancelled {
						return false
					}
				}
				if root {
					_ = os.Remove(filepath.Join(from, UndoRecordFileName))
				}
				// The directory itself is emptied and removed: count it like
				// UndoCounts does (the root backup dir is not an entry of it).
				if os.Remove(from) == nil {
					if !root {
						result.Restored++
						done++
					}
					return true
				}
				return false
			}
			if err := os.Rename(from, to); err != nil {
				result.Failed++
				result.Problems = append(result.Problems, fmt.Sprintf("无法还原 %s：%v", displayPath(to), err))
				return false
			}
			result.Restored += countAs
			done += countAs
			report(permille(done, total), "正在还原 "+displayPath(to))
			return true
		}
		restore(backup, rec.Destination, true)
		// Whatever could not be moved back stays in the backup dir.
		if isRealDir(backup) && len(listDir(backup)) > 0 {
			result.LeftInBackupDir = rec.UndoDir()
			if !result.Cancelled {
				result.Failed++
			}
		}
	}

	report(1000, "")
	return result
}

func permille(done, total int) int {
	if total < 1 {
		return 0
	}
	v := 1000 * done / total
	if v < 0 {
		return 0
	}
	if v > 1000 {
		return 1000
	}
	return v
}

// UndoDiscard deletes the backup dir without undoing anything (before the next
// copy run, so that rsync does not see it as part of the destination).
func UndoDiscard(rec UndoRecord) ([]string, bool) {
	if !rec.Valid() {
		return nil, true
	}
	backup := rec.UndoDir()
	info, ok := lstat(backup)
	if !ok {
		return nil, true
	}
	var problems []string
	removed := 0
	cancelled := false
	device, _ := deviceOf(info)
	removeTree(backup, device, &removed, &problems, context.Background(), &cancelled)
	if _, still := lstat(backup); still {
		return problems, false
	}
	return problems, true
}

// escapeForRecord converts bytes that are not valid UTF-8 into the private-use
// characters U+F780 + (byte - 0x80), the same representation as the Qt
// application (its LocalPath): json.Marshal would otherwise replace such bytes
// with U+FFFD, and the record must survive with the exact bytes of the name.
func escapeForRecord(s string) string {
	if utf8.ValidString(s) && !strings.ContainsRune(s, 0xF780) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			b.WriteByte(c)
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// An invalid byte: keep it as a private-use character.
			b.WriteRune(rune(0xF780 + int(c) - 0x80))
			i++
			continue
		}
		if r >= 0xF780 && r <= 0xF7FF {
			// The same sequence as a real character would be ambiguous: escape
			// every byte of it, like the Qt application does.
			for j := 0; j < size; j++ {
				b.WriteRune(rune(0xF780 + int(s[i+j]) - 0x80))
			}
			i += size
			continue
		}
		b.WriteString(s[i : i+size])
		i += size
	}
	return b.String()
}

// unescapeRecord is the inverse of escapeForRecord.
func unescapeRecord(s string) string {
	hasPrivate := false
	for _, r := range s {
		if r >= 0xF780 && r <= 0xF7FF {
			hasPrivate = true
			break
		}
	}
	if !hasPrivate {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if r >= 0xF780 && r <= 0xF7FF {
			b.WriteByte(byte(0x80 + int(r) - 0xF780))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// EscapeForRecord makes a path with bytes that are not valid UTF-8 safe for the
// JSON configuration (see escapeForRecord).
func EscapeForRecord(path string) string { return escapeForRecord(path) }

// UnescapeForRecord is the inverse of EscapeForRecord.
func UnescapeForRecord(path string) string { return unescapeRecord(path) }

// UndoSave stores the record inside its backup dir.
func UndoSave(rec UndoRecord) error {
	if !rec.Valid() {
		return fmt.Errorf("无效的撤回记录")
	}
	if err := os.MkdirAll(rec.UndoDir(), 0o755); err != nil {
		return err
	}
	if rec.FinishedAt == "" {
		rec.FinishedAt = time.Now().Format(time.RFC3339)
	}
	rec.Destination = escapeForRecord(rec.Destination)
	for i, name := range rec.NewEntries {
		rec.NewEntries[i] = escapeForRecord(name)
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return os.WriteFile(rec.RecordPath(), data, 0o644)
}

// UndoLoad reads a record written by UndoSave (or by the Qt application, whose
// representation of invalid bytes is the same).
func UndoLoad(path string) (UndoRecord, bool) {
	var rec UndoRecord
	data, err := os.ReadFile(path)
	if err != nil {
		return rec, false
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		return rec, false
	}
	if !rec.Valid() {
		return UndoRecord{}, false
	}
	rec.Destination = unescapeRecord(rec.Destination)
	for i, name := range rec.NewEntries {
		rec.NewEntries[i] = unescapeRecord(name)
	}
	if path != rec.RecordPath() {
		return UndoRecord{}, false
	}
	return rec, true
}

func displayPath(path string) string {
	return location.DisplayPath(path)
}

// DisplayRelPath shows a recorded entry name (relative, may contain bytes that
// are not valid UTF-8).
func DisplayRelPath(name string) string {
	return location.DisplayPath(name)
}

// uniqueNames removes duplicates while keeping the order of the first occurrence.
func uniqueNames(names []string) []string {
	seen := make(map[string]bool, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}
