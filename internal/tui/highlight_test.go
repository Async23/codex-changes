package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Async23/codex-changes/internal/changes"
	"github.com/Async23/codex-changes/internal/highlight"
	"github.com/charmbracelet/x/ansi"
)

func TestHighlightCacheSurvivesLayoutButInvalidatesContent(t *testing.T) {
	m := demoModel(t)
	edit := &m.selectedFile().Edits[0]
	first := m.highlights.get(m.selectedPath(), *edit)
	typeKey(m, 't')
	typeKey(m, 'v')
	typeKey(m, 'w')
	if got := m.highlights.get(m.selectedPath(), *edit); got != first {
		t.Fatal("layout reparsed the patch")
	}
	edit.Lines[1].Text += " updated"
	if got := m.highlights.get(m.selectedPath(), *edit); got == first {
		t.Fatal("changed history reused stale syntax")
	}
	if got := m.highlights.get("different.py", *edit); got == first || len(m.highlights.edits) != 1 {
		t.Fatal("cache was not scoped to the selected file")
	}
}

func TestStyledWrapPreservesGraphemesWhitespaceAndWordColor(t *testing.T) {
	text := "  中文 é 👩🏽‍💻\tend  "
	spans := []highlight.Span{{Text: text, Role: highlight.String, Changed: true}}
	for _, width := range []int{4, 5, 7, 12, 40} {
		var reconstructed strings.Builder
		for _, row := range layoutSpans(spans, width, true, 0) {
			var visible strings.Builder
			for _, span := range row {
				if !span.Changed || span.Role != highlight.String {
					t.Fatal("wrapped fragment lost style")
				}
				visible.WriteString(span.Text)
			}
			if ansi.StringWidth(visible.String()) > width {
				t.Fatal("wrapped row overflows")
			}
			reconstructed.WriteString(visible.String())
		}
		if reconstructed.String() != strings.ReplaceAll(text, "\t", "    ") {
			t.Fatalf("wrap lost whitespace or split a glyph: %q", reconstructed.String())
		}
	}
	rows := layoutSpans([]highlight.Span{{Text: "甲乙丙"}}, 4, false, 1)
	if rows[0][0].Text != " 乙 " {
		t.Fatalf("horizontal clipping split a CJK glyph: %+v", rows)
	}
}

func TestRenderedBackgroundsFillRowsAndKeepSyntax(t *testing.T) {
	m := demoModel(t)
	input, _, _ := changes.ParseDiff("update", "@@ -99,1 +99,1 @@\n-const filename = 'draft.svg';\n+const filename = 'final.svg';\n")
	d := highlight.Analyze("sample.ts", input)
	for _, theme := range []string{"light", "dark"} {
		m.opts.Preferences.Theme = theme
		p := m.palette()
		for i, want := range []string{"draft", "final"} {
			line := &d.Lines[i+1]
			base, word := p.removeBG, p.removeWord
			if i == 1 {
				base, word = p.addBG, p.addWord
			}
			baseRGB, wordRGB := colorRGB(base), colorRGB(word)
			for _, width := range []int{17, 60} {
				rows := m.codeRows(line, width, d.Digits, false)
				var highlighted strings.Builder
				for n, row := range rows {
					// Include the outer application frame to catch nested reset leakage.
					row = style(p.fg).Background(p.bg).Render(row)
					if ansi.StringWidth(row) != width {
						t.Fatalf("row width: %d", ansi.StringWidth(row))
					}
					cells := ansiCells(row)
					for _, cell := range cells {
						if cell.bg != baseRGB && cell.bg != wordRGB {
							t.Fatalf("%s: background hole at %q: %s", theme, cell.text, cell.bg)
						}
						if cell.bg == wordRGB {
							highlighted.WriteString(cell.text)
						}
					}
					if cells[len(cells)-1].text == " " && cells[len(cells)-1].bg != baseRGB {
						t.Fatal("word background leaked into padding")
					}
					if n == 0 && !strings.HasPrefix(ansi.Strip(row), " 99 ") {
						t.Fatal("wrong single-column line number")
					}
				}
				if highlighted.String() != want {
					t.Fatalf("highlighted %q, want %q", highlighted.String(), want)
				}
			}
		}
	}
}

type ansiCell struct{ text, bg string }

var sgr = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

func ansiCells(text string) []ansiCell {
	var result []ansiCell
	bg, position := "", 0
	appendText := func(s string) {
		for _, r := range s {
			result = append(result, ansiCell{string(r), bg})
		}
	}
	for _, match := range sgr.FindAllStringSubmatchIndex(text, -1) {
		appendText(text[position:match[0]])
		codes := strings.Split(text[match[2]:match[3]], ";")
		for i := 0; i < len(codes); i++ {
			n, _ := strconv.Atoi(codes[i])
			if n == 0 || n == 49 {
				bg = ""
			}
			if (n == 38 || n == 48) && i+4 < len(codes) && codes[i+1] == "2" {
				if n == 48 {
					bg = strings.Join(codes[i+2:i+5], ",")
				}
				i += 4
			}
		}
		position = match[1]
	}
	appendText(text[position:])
	return result
}

func colorRGB(c interface {
	RGBA() (uint32, uint32, uint32, uint32)
}) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("%d,%d,%d", r>>8, g>>8, b>>8)
}
