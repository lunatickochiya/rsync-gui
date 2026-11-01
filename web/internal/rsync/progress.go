// Package rsync contains the transfer planner and runner.
package rsync

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Progress is one parsed rsync progress line.
type Progress struct {
	Transferred    string
	Percent        int
	Rate           string
	ETA            string
	TransferNumber int // -1 when absent
	Remaining      int
	Total          int
	HasTotal       bool
	Bytes          int64
	Speed          int64 // bytes per second, -1 when unknown
}

var progressRE = regexp.MustCompile(
	`^\s*([0-9][0-9.,]*[A-Za-z]?)\s+([0-9]{1,3})%\s+(\S+)\s+([0-9]+:[0-9]{2}:[0-9]{2})(?:\s+\((?:xfr#([0-9]+),\s*)?(to|ir)-chk=([0-9]+)/([0-9]+)\))?\s*$`)

// ParseProgressLine parses "3,00M 99% 2,76GB/s 0:00:00 (xfr#1, to-chk=3/5)".
func ParseProgressLine(line string, humanLevel int) (Progress, bool) {
	m := progressRE.FindStringSubmatch(line)
	if m == nil {
		return Progress{}, false
	}
	p := Progress{
		Transferred:    m[1],
		Rate:           m[3],
		ETA:            m[4],
		TransferNumber: -1,
		Bytes:          -1,
		Speed:          -1,
	}
	p.Percent, _ = strconv.Atoi(m[2])
	if p.Percent < 0 {
		p.Percent = 0
	} else if p.Percent > 100 {
		p.Percent = 100
	}
	if m[5] != "" {
		p.TransferNumber, _ = strconv.Atoi(m[5])
	}
	if m[6] != "" {
		p.HasTotal = true
		p.Remaining, _ = strconv.Atoi(m[7])
		p.Total, _ = strconv.Atoi(m[8])
	}
	p.Bytes = ParseSize(p.Transferred, humanLevel)
	p.Speed = ParseRate(p.Rate)
	return p, true
}

// ParseSize parses a human-readable size such as "1.23G", "4,62MB" or "1234".
func ParseSize(text string, humanLevel int) int64 {
	if text == "" {
		return -1
	}
	power := unitPower(text[len(text)-1])
	if power == 0 {
		var digits strings.Builder
		for _, r := range text {
			if r >= '0' && r <= '9' {
				digits.WriteRune(r)
			}
		}
		v, err := strconv.ParseInt(digits.String(), 10, 64)
		if err != nil {
			return -1
		}
		return v
	}
	value := parseDecimal(text[:len(text)-1])
	if value < 0 {
		return -1
	}
	base := 1000.0
	if humanLevel >= 3 {
		base = 1024.0
	}
	return int64(value * math.Pow(base, float64(power)))
}

// ParseRate parses "2,76MB/s". Transfer rates use 1024-based units.
func ParseRate(text string) int64 {
	re := regexp.MustCompile(`^([0-9]+(?:[.,][0-9]+)?)([kKMGTPE])B/s$`)
	m := re.FindStringSubmatch(text)
	if m == nil {
		return -1
	}
	value := parseDecimal(m[1])
	if value < 0 {
		return -1
	}
	return int64(value * math.Pow(1024, float64(unitPower(m[2][0]))))
}

func parseDecimal(number string) float64 {
	number = strings.ReplaceAll(number, ",", ".")
	v, err := strconv.ParseFloat(number, 64)
	if err != nil {
		return -1
	}
	return v
}

func unitPower(unit byte) int {
	units := "KMGTPE"
	for i := 0; i < len(units); i++ {
		if unit == units[i] || unit == units[i]+32 {
			return i + 1
		}
	}
	return 0
}

var differenceRE = regexp.MustCompile(`^[<>ch][fdLDS][^ ]{9,} (.+)$`)

// ParseDifferenceLine parses an --itemize-changes line from the verification pass.
func ParseDifferenceLine(line string) (string, bool) {
	m := differenceRE.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// HumanLevel counts -h flags in an argument list, like rsync-gui.
func HumanLevel(args []string) int {
	level := 1
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--human-readable" {
			level++
			continue
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			level += strings.Count(arg, "h")
		}
	}
	return level
}
