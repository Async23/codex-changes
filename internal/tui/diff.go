package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Async23/codex-changes/internal/changes"
	"github.com/charmbracelet/x/ansi"
)

type displayRow struct {
	text, editID string
	header       bool
}

func (m *model) rebuildDiff() {
	m.rows, m.anchors = nil, nil
	file := m.selectedFile()
	if file == nil {
		m.diffOffset = 0
		return
	}
	p, width := m.palette(), m.diffWidth()
	for _, edit := range file.Edits {
		if len(m.rows) > 0 {
			m.rows = append(m.rows, displayRow{editID: edit.ID})
		}
		m.anchors = append(m.anchors, len(m.rows))
		stamp := "时间未记录"
		if edit.Time != 0 {
			stamp = time.Unix(edit.Time, 0).Local().Format("2006-01-02 15:04")
		}
		kind := map[string]string{"add": "新建", "delete": "删除", "update": "修改"}[edit.Kind]
		if kind == "" {
			kind = "修改"
		}
		if edit.From != "" {
			kind = "移动"
		}
		mark := "▾"
		if m.collapsed[edit.ID] {
			mark = "▸"
		}
		label := fmt.Sprintf(" %s %s  %s · 第 %d 轮", mark, stamp, kind, edit.TurnNumber)
		right := style(p.add).Render(fmt.Sprintf("+%d", edit.Added)) + " " + style(p.remove).Render(fmt.Sprintf("−%d", edit.Removed)) + " "
		text := style(p.accent).Bold(true).Background(p.selected).Render(between(label, right, width))
		m.rows = append(m.rows, displayRow{text: text, editID: edit.ID, header: true})
		if m.collapsed[edit.ID] {
			continue
		}
		if edit.ThreadID != m.session.Thread.ID {
			m.addTextRows("子会话 · "+clean(edit.Source), edit.ID, width)
		}
		if edit.From != "" {
			m.addTextRows("原路径 · "+clean(edit.From), edit.ID, width)
		}
		if edit.Prompt != "" {
			m.addTextRows("你说 · "+edit.Prompt, edit.ID, width)
		}
		if len(edit.Lines) == 0 {
			m.addTextRows("这条记录没有差异正文。", edit.ID, width)
		}
		prepared := m.highlights.get(file.Path, edit)
		if m.opts.Preferences.Split && width >= 60 {
			leftWidth := (width - 1) / 2
			for _, pair := range prepared.Pairs {
				if pair.Left != nil && (pair.Left.Kind == changes.Hunk || pair.Left.Kind == changes.Metadata) {
					for _, row := range m.metadataRows(pair.Left.Line, width) {
						m.rows = append(m.rows, displayRow{text: row, editID: edit.ID})
					}
					continue
				}
				left, right := m.codeRows(pair.Left, leftWidth, prepared.Digits, true), m.codeRows(pair.Right, width-leftWidth-1, prepared.Digits, false)
				for i := 0; i < max(len(left), len(right)); i++ {
					l, r := strings.Repeat(" ", leftWidth), strings.Repeat(" ", width-leftWidth-1)
					if i < len(left) {
						l = left[i]
					}
					if i < len(right) {
						r = right[i]
					}
					m.rows = append(m.rows, displayRow{text: l + style(p.border).Render("│") + r, editID: edit.ID})
				}
			}
		} else {
			for i := range prepared.Lines {
				line := &prepared.Lines[i]
				var rows []string
				if line.Kind == changes.Hunk || line.Kind == changes.Metadata {
					rows = m.metadataRows(line.Line, width)
				} else {
					rows = m.codeRows(line, width, prepared.Digits, false)
				}
				for _, row := range rows {
					m.rows = append(m.rows, displayRow{text: row, editID: edit.ID})
				}
			}
		}
	}
	m.clamp()
}

func (m *model) addTextRows(text, id string, width int) {
	for _, line := range strings.Split(ansi.Wrap(clean(text), max(1, width-4), " "), "\n") {
		m.rows = append(m.rows, displayRow{text: style(m.palette().muted).Render(fit("  "+line, width)), editID: id})
	}
}

func (m *model) pieces(text string, width int) []string {
	text = strings.ReplaceAll(text, "\t", "    ")
	width = max(1, width)
	if m.opts.Preferences.Wrap {
		return strings.Split(ansi.Hardwrap(text, width, true), "\n")
	}
	return []string{ansi.Cut(text, m.diffColumn, m.diffColumn+width)}
}

func (m *model) metadataRows(line changes.Line, width int) []string {
	p := m.palette()
	c := p.muted
	if line.Kind == changes.Hunk {
		c = p.accent
	}
	var rows []string
	for _, text := range m.pieces(line.Text, width-2) {
		rows = append(rows, style(c).Render(fit("  "+text, width)))
	}
	return rows
}

func (m *model) maxColumn() int {
	file := m.selectedFile()
	if file == nil {
		return 0
	}
	width := m.diffWidth()
	if m.opts.Preferences.Split && width >= 60 {
		width = (width - 1) / 2
	}
	maximum := 0
	for _, edit := range file.Edits {
		digits := 1
		for _, line := range edit.Lines {
			digits = max(digits, len(strconv.Itoa(max(line.Old, line.New))))
		}
		for _, line := range edit.Lines {
			available := width - digits - 4
			if line.Kind == changes.Hunk || line.Kind == changes.Metadata {
				available = m.diffWidth() - 2
			}
			maximum = max(maximum, ansi.StringWidth(strings.ReplaceAll(line.Text, "\t", "    "))-available)
		}
	}
	return max(0, maximum)
}
