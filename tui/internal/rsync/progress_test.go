package rsync

import (
	"math"
	"strings"
	"testing"

	"github.com/lunatickochiya/rsync-gui/tui/internal/config"
)

func TestParseProgressLine(t *testing.T) {
	line := `      3,00M  99%    2,76GB/s    0:00:00 (xfr#1, to-chk=3/5)`
	p, ok := ParseProgressLine(line, 1)
	if !ok {
		t.Fatal("not parsed")
	}
	if p.Percent != 99 || p.TransferNumber != 1 || p.Remaining != 3 || p.Total != 5 || !p.HasTotal {
		t.Fatalf("progress = %+v", p)
	}
	if p.Bytes != 3_000_000 {
		t.Fatalf("bytes = %d", p.Bytes)
	}
	wantRate := int64(2.76 * math.Pow(1024, 3))
	if p.Speed != wantRate {
		t.Fatalf("speed = %d, want %d", p.Speed, wantRate)
	}
}

func TestParseProgressOverall(t *testing.T) {
	p, ok := ParseProgressLine("  1.23G  45%   12.34MB/s    0:00:12 (xfr#123, to-chk=45/678)", 1)
	if !ok || p.Percent != 45 || !p.HasTotal || p.TransferNumber != 123 {
		t.Fatalf("progress = %+v ok=%v", p, ok)
	}
	p, ok = ParseProgressLine("  1.23G  45%   12.34MB/s    0:00:12", 1)
	if !ok || p.TransferNumber != -1 {
		t.Fatalf("progress = %+v ok=%v", p, ok)
	}
	if _, ok := ParseProgressLine("sending incremental file list", 1); ok {
		t.Fatal("false positive")
	}
}

func TestParseDifference(t *testing.T) {
	name, ok := ParseDifferenceLine(">f+++++++++ dir/file with space.txt")
	if !ok || name != "dir/file with space.txt" {
		t.Fatalf("name=%q ok=%v", name, ok)
	}
	if _, ok := ParseDifferenceLine("sending incremental file list"); ok {
		t.Fatal("false positive")
	}
}

func TestHumanLevel(t *testing.T) {
	if got := HumanLevel([]string{"-avhP", "--", "-hhh"}); got != 2 {
		t.Fatalf("level = %d", got)
	}
}

func TestStripProgressPrefix(t *testing.T) {
	line := " 44.60M  35%    8.00MB/s    0:00:09  rsync error: received SIGINT"
	got := stripProgressPrefix(line)
	if !strings.HasPrefix(got, "rsync error:") {
		t.Fatalf("got %q", got)
	}
	if got := stripProgressPrefix("normal file name"); got != "normal file name" {
		t.Fatalf("got %q", got)
	}
}

func TestProgressTracker(t *testing.T) {
	tr := newProgressTracker(optionsForTest("per-file"))
	if got := tr.percent(Progress{Percent: 50, Remaining: 1, Total: 2, HasTotal: true}); math.Abs(got-50) > 0.01 {
		t.Fatalf("first file = %v", got)
	}
	// The final to-chk line keeps one extra step of headroom; the UI sets 100%
	// when the process finishes successfully.
	if got := tr.percent(Progress{Percent: 50, Remaining: 0, Total: 2, HasTotal: true}); math.Abs(got-66.67) > 0.1 {
		t.Fatalf("second file = %v", got)
	}
	tr = newProgressTracker(optionsForTest("overall"))
	if got := tr.percent(Progress{Percent: 42}); math.Abs(got-42) > 0.01 {
		t.Fatalf("overall = %v", got)
	}
}

func optionsForTest(progress string) config.Options {
	o := config.DefaultOptions()
	o.Progress = progress
	return o
}
