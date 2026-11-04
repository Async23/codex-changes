package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Async23/codex-changes/internal/changes"
	"github.com/Async23/codex-changes/internal/highlight"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

type cachedHighlight struct {
	input []changes.Line
	diff  *highlight.Diff
}

// Cache only the selected file, bounded by its history. Color and width do not
// invalidate syntax/word analysis; replaced scan data does. Nothing is persisted.
type highlightCache struct {
	path  string
	edits map[string]cachedHighlight
}

func (c *highlightCache) get(path string, edit changes.Edit) *highlight.Diff {
	if c.path != path || c.edits == nil {
		c.path, c.edits = path, make(map[string]cachedHighlight)
	}
	if cached, ok := c.edits[edit.ID]; ok && slices.Equal(cached.input, edit.Lines) {
		return cached.diff
	}
	diff := highlight.Analyze(path, edit.Lines)
	c.edits[edit.ID] = cachedHighlight{input: slices.Clone(edit.Lines), diff: diff}
	return diff
}

// layoutSpans works in terminal cells and never cuts a grapheme. Every wrapped
// fragment carries its own syntax/word style, independent of preceding ANSI.
func layoutSpans(spans []highlight.Span, width int, wrap bool, column int) [][]highlight.Span {
	width = max(1, width)
	rows := [][]highlight.Span{{}}
	used, position := 0, 0
	appendText := func(span highlight.Span, text string) {
		i := len(rows) - 1
		row := rows[i]
		if len(row) > 0 && row[len(row)-1].Role == span.Role && row[len(row)-1].Changed == span.Changed {
			row[len(row)-1].Text += text
		} else {
			span.Text = text
			row = append(row, span)
		}
		rows[i] = row
	}
	for _, span := range spans {
		g := uniseg.NewGraphemes(strings.ReplaceAll(span.Text, "\t", "    "))
		for g.Next() {
			text, size := g.Str(), ansi.StringWidth(g.Str())
			if wrap {
				if used+size > width && used > 0 {
					rows = append(rows, nil)
					used = 0
				}
				if size > width {
					text, size = "…", 1
				}
				appendText(span, text)
				used += size
				continue
			}
			start, end := position, position+size
			position = end
			if end <= column {
				continue
			}
			if start >= column+width {
				return rows
			}
			if start < column || end > column+width {
				// A viewport may start/end inside a wide glyph. Keep the cells
				// aligned with spaces instead of emitting half a character.
				text = strings.Repeat(" ", min(end, column+width)-max(start, column))
			}
			appendText(span, text)
		}
	}
	return rows
}

func (m *model) codeRows(line *highlight.Line, width, digits int, left bool) []string {
	if line == nil {
		return []string{strings.Repeat(" ", width)}
	}
	p := m.palette()
	lineNumber, mark, decoration, bg, wordBG := line.New, " ", p.muted, p.bg, p.bg
	if left || line.Kind == changes.Removed {
		lineNumber = line.Old
	}
	switch line.Kind {
	case changes.Added:
		mark, decoration, bg, wordBG = "+", p.add, p.addBG, p.addWord
	case changes.Removed:
		mark, decoration, bg, wordBG = "−", p.remove, p.removeBG, p.removeWord
	}
	prefixWidth := digits + 4 // left margin, number, space, marker, space
	contentWidth := max(1, width-prefixWidth)
	var rows []string
	for i, spans := range layoutSpans(line.Spans, contentWidth, m.opts.Preferences.Wrap, m.diffColumn) {
		gutter := fmt.Sprintf(" %*d %s ", digits, lineNumber, mark)
		if i > 0 {
			gutter = strings.Repeat(" ", digits+2) + "↳ "
		}
		var row strings.Builder
		row.WriteString(style(decoration).Background(bg).Render(gutter))
		used := 0
		for _, span := range spans {
			background := bg
			if span.Changed {
				background = wordBG
			}
			row.WriteString(style(p.syntax[span.Role]).Background(background).Render(span.Text))
			used += ansi.StringWidth(span.Text)
		}
		// Pad with the line background, not the last token's word background.
		row.WriteString(style(p.syntax[highlight.Plain]).Background(bg).Render(strings.Repeat(" ", max(0, contentWidth-used))))
		rows = append(rows, row.String())
	}
	return rows
}
