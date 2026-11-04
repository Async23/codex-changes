package changes

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

type LineKind uint8

const (
	Context LineKind = iota
	Added
	Removed
	Hunk
	Metadata
)

type Line struct {
	Kind LineKind `json:"kind"`
	Text string   `json:"text"`
	Old  int      `json:"old,omitempty"`
	New  int      `json:"new,omitempty"`
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// SafeText removes terminal control sequences from untrusted history before
// displaying it. Newlines and tabs are retained for layout, never executed.
func SafeText(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, ansi.Strip(value))
}

func OneLine(value string) string {
	return strings.Join(strings.Fields(SafeText(value)), " ")
}

// ParseDiff interprets app-server add/delete bodies as complete file contents;
// updates are unified patches. Counting is cumulative, not a net Git diff.
func ParseDiff(kind, body string) ([]Line, int, int) {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	if body == "" {
		return nil, 0, 0
	}
	rows := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	lines := make([]Line, 0, len(rows))
	added, removed, old, next := 0, 0, 0, 0
	insideHunk := false
	for i, raw := range rows {
		line := Line{Text: SafeText(raw), Kind: Metadata}
		if kind == "add" {
			line.Kind, line.New = Added, i+1
			added++
		} else if kind == "delete" {
			line.Kind, line.Old = Removed, i+1
			removed++
		} else if match := hunkHeader.FindStringSubmatch(raw); match != nil {
			old, _ = strconv.Atoi(match[1])
			next, _ = strconv.Atoi(match[2])
			line.Kind, insideHunk = Hunk, true
		} else if insideHunk && strings.HasPrefix(raw, "+") {
			line.Kind, line.New, line.Text = Added, next, SafeText(raw[1:])
			next++
			added++
		} else if insideHunk && strings.HasPrefix(raw, "-") {
			line.Kind, line.Old, line.Text = Removed, old, SafeText(raw[1:])
			old++
			removed++
		} else if insideHunk && strings.HasPrefix(raw, " ") {
			line.Kind, line.Old, line.New, line.Text = Context, old, next, SafeText(raw[1:])
			old++
			next++
		} else if strings.HasPrefix(raw, "diff --git ") {
			insideHunk = false
		}
		lines = append(lines, line)
	}
	return lines, added, removed
}

type Pair struct{ Left, Right *Line }

// SideBySide aligns contiguous replacement blocks without inventing context.
func SideBySide(lines []Line) []Pair {
	var result []Pair
	for i := 0; i < len(lines); {
		if lines[i].Kind != Added && lines[i].Kind != Removed {
			result = append(result, Pair{Left: &lines[i], Right: &lines[i]})
			i++
			continue
		}
		var before, after []*Line
		for i < len(lines) && (lines[i].Kind == Added || lines[i].Kind == Removed) {
			if lines[i].Kind == Removed {
				before = append(before, &lines[i])
			} else {
				after = append(after, &lines[i])
			}
			i++
		}
		for j := 0; j < max(len(before), len(after)); j++ {
			p := Pair{}
			if j < len(before) {
				p.Left = before[j]
			}
			if j < len(after) {
				p.Right = after[j]
			}
			result = append(result, p)
		}
	}
	return result
}
