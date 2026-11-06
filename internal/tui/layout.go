package tui

import "github.com/charmbracelet/x/ansi"

func (m *model) listUnavailable(level navLevel) bool {
	return level >= levelDirectory && m.session.Thread.ID == "" ||
		level == levelDirectory && m.level == levelSession && m.selectedThreadID() != m.session.Thread.ID
}

func (m *model) emptyListMessage(level navLevel) string {
	if m.listLoading || m.loading {
		return "正在读取…"
	}
	if m.listUnavailable(level) {
		return "选择会话后自动读取"
	}
	if m.currentQueryFor(level) != "" {
		return "当前栏没有匹配项"
	}
	return "没有可显示的项目"
}

func (m *model) desiredColumnWidth(c column) int {
	if c.diff {
		return 50
	}
	// Measure terminal cells, not bytes or rune counts. Include the selection
	// marker, the metadata gap and right padding, just as navigationItem does.
	width := 2 + ansi.StringWidth(m.listHeading(c.level)) + 1
	if m.listUnavailable(c.level) || m.navigationCount(c.level) == 0 {
		return max(16, min(24, width))
	}
	start := m.listOffset(c.level)
	end := min(m.navigationCount(c.level), start+m.listPage())
	for i := start; i < end; i++ {
		name, info := m.navigationParts(c.level, i)
		rowWidth := 2 + ansi.StringWidth(name) + 1
		if info != "" {
			rowWidth += 2 + ansi.StringWidth(info)
		}
		width = max(width, rowWidth)
	}
	return min(80, max(16, width))
}

func columnMinimum(c column) int {
	if c.diff {
		return 50
	}
	if c.level == levelSession || c.level == levelFile || c.level == levelEdit {
		return min(c.width, 26)
	}
	return min(c.width, 20)
}

func (m *model) fitColumns(columns []column) []column {
	for i := range columns {
		columns[i].width = m.desiredColumnWidth(columns[i])
	}
	minimum := func() int {
		width := len(columns) - 1
		for _, c := range columns {
			width += columnMinimum(c)
		}
		return width
	}
	for len(columns) > 1 && minimum() > m.width {
		// Keep the active list and the preview; remove older ancestors first.
		if !columns[0].diff && columns[0].level < m.level {
			columns = columns[1:]
		} else {
			columns = columns[:len(columns)-1]
		}
	}
	available := m.width - len(columns) + 1
	total := 0
	for _, c := range columns {
		total += c.width
	}
	// Share pressure across columns with spare width instead of assigning
	// fixed percentages.
	for total > available {
		widest, spare := -1, 0
		for i, c := range columns {
			if room := c.width - columnMinimum(c); room > spare {
				widest, spare = i, room
			}
		}
		if widest < 0 {
			break
		}
		columns[widest].width--
		total--
	}
	// Like Yazi, fit the parent and current lists to their content and give
	// all remaining space to the right preview, including directory previews.
	columns[len(columns)-1].width += available - total
	return columns
}
