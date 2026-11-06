package tui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Async23/codex-changes/internal/changes"
	"github.com/Async23/codex-changes/internal/highlight"
	"github.com/charmbracelet/x/ansi"
)

type palette struct {
	bg, fg, muted, border, accent, selected, add, remove, addBG, removeBG color.Color
	addWord, removeWord                                                   color.Color
	syntax                                                                [highlight.RoleCount]color.Color
}

func (m *model) palette() palette {
	dark := m.dark
	if m.opts.Preferences.Theme != "auto" {
		dark = m.opts.Preferences.Theme == "dark"
	}
	c := lipgloss.Color
	if dark {
		return palette{
			bg: c("#171a1d"), fg: c("#e5e9eb"), muted: c("#9ba5ac"), border: c("#414950"), accent: c("#84cfc2"), selected: c("#293c3c"),
			add: c("#72ce88"), remove: c("#e88383"), addBG: c("#123019"), removeBG: c("#401b20"), addWord: c("#245c30"), removeWord: c("#753038"),
			syntax: [highlight.RoleCount]color.Color{c("#f8f8f2"), c("#f92672"), c("#66d9ef"), c("#e6db74"), c("#be84ff"), c("#a0a194"), c("#a6e22e"), c("#a6e22e"), c("#f92672"), c("#f92672"), c("#66d9ef")},
		}
	}
	return palette{
		bg: c("#faf4ed"), fg: c("#333333"), muted: c("#7c817a"), border: c("#d6d1c9"), accent: c("#087568"), selected: c("#e3eee7"),
		add: c("#248a3d"), remove: c("#cf222e"), addBG: c("#dcffdc"), removeBG: c("#ffdcdc"), addWord: c("#b2ffb2"), removeWord: c("#ffc7c7"),
		syntax: [highlight.RoleCount]color.Color{c("#333333"), c("#a71d5d"), c("#a71d5d"), c("#183691"), c("#0086b3"), c("#7c817a"), c("#795da3"), c("#0086b3"), c("#333333"), c("#a71d5d"), c("#0086b3")},
	}
}

func style(fg color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(fg) }
func fit(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = ansi.Truncate(value, width, "…")
	return value + strings.Repeat(" ", max(0, width-ansi.StringWidth(value)))
}
func cut(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = ansi.Cut(value, 0, width)
	return value + strings.Repeat(" ", max(0, width-ansi.StringWidth(value)))
}
func between(left, right string, width int) string {
	if ansi.StringWidth(right)+2 >= width {
		return fit(left, width)
	}
	return fit(left, width-ansi.StringWidth(right)) + right
}
func clean(value string) string { return changes.OneLine(value) }

func (m *model) View() tea.View {
	content := m.render()
	v := tea.NewView(content)
	p := m.palette()
	v.AltScreen, v.MouseMode = true, tea.MouseModeCellMotion
	v.BackgroundColor, v.ForegroundColor = p.bg, p.fg
	return v
}

func (m *model) render() string {
	w, h := max(1, m.width), max(1, m.height)
	p := m.palette()
	if w < 40 || h < 10 {
		return fit("请把终端放大到 40 × 10；Ctrl+C 退出", w)
	}
	var rows []string
	if m.help {
		rows = m.helpRows()
	} else {
		rows = m.mainRows()
	}
	for len(rows) < h {
		rows = append(rows, "")
	}
	if len(rows) > h {
		rows = rows[:h]
	}
	for i, row := range rows {
		rows[i] = style(p.fg).Background(p.bg).Render(fit(row, w))
	}
	return strings.Join(rows, "\n")
}

func (m *model) brandRow() string {
	p := m.palette()
	label := "本机历史 · 只读"
	if m.opts.Demo {
		label = "DEMO · 合成数据"
	}
	left := " " + style(p.accent).Bold(true).Render("CODEX CHANGES") + "  " + style(p.muted).Render(label)
	right := "s 会话  ? 帮助 "
	if m.level <= levelSession && !m.focusDiff && !m.help {
		right = "输入搜索  Ctrl+C 退出 "
	}
	return between(left, style(p.muted).Render(right), m.width)
}

type column struct {
	level navLevel
	diff  bool
	width int
}

func navColumn(level navLevel) column { return column{level: level} }
func diffColumn() column              { return column{diff: true} }

func columnMinimum(c column) int {
	if c.diff {
		return 50
	}
	switch c.level {
	case levelFile, levelSession:
		return 26
	case levelEdit:
		return 30
	default:
		return 20
	}
}

func columnDesired(c column) int {
	if c.diff {
		return 72
	}
	switch c.level {
	case levelProject, levelDirectory:
		return 26
	case levelSession, levelFile:
		return 34
	case levelEdit:
		return 38
	case levelHunk:
		return 34
	}
	return 30
}

func columnsWidth(columns []column, desired bool) int {
	total := len(columns) - 1
	for _, c := range columns {
		if desired {
			total += columnDesired(c)
		} else {
			total += columnMinimum(c)
		}
	}
	return total
}

func (m *model) columns() []column {
	if m.hideFiles {
		return []column{{diff: true, width: m.width}}
	}
	var columns []column
	if m.continuous {
		columns = []column{navColumn(levelFile), diffColumn()}
	} else {
		switch m.level {
		case levelProject:
			columns = []column{navColumn(levelProject), navColumn(levelSession)}
		case levelSession:
			columns = []column{navColumn(levelProject), navColumn(levelSession), navColumn(levelDirectory)}
		case levelDirectory:
			columns = []column{navColumn(levelSession), navColumn(levelDirectory), navColumn(levelFile)}
		case levelFile:
			columns = []column{navColumn(levelDirectory), navColumn(levelFile), navColumn(levelEdit)}
		case levelEdit:
			columns = []column{navColumn(levelFile), navColumn(levelEdit), navColumn(levelHunk)}
		case levelHunk:
			columns = []column{navColumn(levelEdit), navColumn(levelHunk), diffColumn()}
		}
	}
	if m.narrow() {
		if m.focusDiff {
			for _, c := range columns {
				if c.diff {
					c.width = m.width
					return []column{c}
				}
			}
		}
		for _, c := range columns {
			if !c.diff && c.level == m.level {
				c.width = m.width
				return []column{c}
			}
		}
	}
	hasDiff := columns[len(columns)-1].diff
	if hasDiff {
		for len(columns) > 2 && columnsWidth(columns, true) > m.width {
			columns = columns[1:]
		}
		remaining := m.width - (len(columns) - 1)
		if len(columns) == 1 {
			columns[0].width = remaining
			return columns
		}
		navigationWidth := 0
		for i := 0; i < len(columns)-1; i++ {
			columns[i].width = columnDesired(columns[i])
			navigationWidth += columns[i].width
		}
		if len(columns) == 2 {
			navigationWidth = min(columnDesired(columns[0]), max(columnMinimum(columns[0]), m.width/3))
			columns[0].width = navigationWidth
		}
		columns[len(columns)-1].width = max(1, remaining-navigationWidth)
		return columns
	}
	remaining := m.width - (len(columns) - 1)
	for i := range columns {
		columns[i].width = remaining / (len(columns) - i)
		remaining -= columns[i].width
	}
	return columns
}

func (m *model) columnAt(x int) *column {
	columns := m.columns()
	start := 0
	for i := range columns {
		if x >= start && x < start+columns[i].width {
			return &columns[i]
		}
		start += columns[i].width + 1
	}
	return nil
}

func (m *model) mainRows() []string {
	p := m.palette()
	title := "选择项目，再选择会话"
	if m.session.Thread.ID != "" && m.level > levelSession {
		parts := []string{clean(m.session.Thread.CWD), clean(m.session.Thread.Title())}
		if d := m.selectedDirectory(); d != nil && m.level >= levelDirectory {
			parts = append(parts, clean(d.Label))
		}
		if file := m.selectedFile(); file != nil && m.level >= levelFile {
			parts = append(parts, clean(filepath.Base(file.Path)))
		}
		title = strings.Join(parts, " › ")
	}
	second := " " + style(p.fg).Bold(true).Render(title)
	rows := []string{m.brandRow(), fit(second, m.width), style(p.border).Render(strings.Repeat("─", m.width))}
	columns := m.columns()
	body := make([][]string, len(columns))
	for i, c := range columns {
		if c.diff {
			body[i] = m.diffRows(c.width)
		} else {
			body[i] = m.navigationRows(c.level, c.width)
		}
	}
	for line := 0; line < m.bodyHeight(); line++ {
		var row strings.Builder
		for i := range columns {
			if i > 0 {
				row.WriteString(style(p.border).Render("│"))
			}
			row.WriteString(body[i][line])
		}
		rows = append(rows, row.String())
	}
	status := m.status
	if m.loading || m.listLoading {
		status = []string{"◐", "◓", "◑", "◒"}[m.spinner] + " " + status
	}
	if m.session.Children > 0 {
		status += fmt.Sprintf(" · 含 %d 个子会话", m.session.Children)
	}
	if m.session.Ignored > 0 {
		status += fmt.Sprintf(" · 跳过 %d 次未成功修改", m.session.Ignored)
	}
	if m.err != "" {
		status = style(p.remove).Render(clean(m.err))
	}
	rows = append(rows, fit(" "+style(p.muted).Render(status), m.width))
	toggle := "c 连续"
	if m.continuous {
		toggle = "c 单块"
	}
	hints := " ↑↓ 选择  ←→ 层级  Enter 展开  / 筛文件  " + toggle + "  s 会话"
	if m.level <= levelSession {
		hints = " ↑↓ 选择  ←→ 层级  Enter 打开  输入筛选  Ctrl+R 刷新"
	} else if m.focusDiff {
		hints = " ↑↓ 滚动  Esc 返回  [ ] 修改  " + toggle + "  v 并排  w 换行"
	}
	if m.narrow() {
		hints = " ↑↓ 移动  Enter 进入  Esc 返回  s 会话  " + toggle
	}
	right := "? 帮助  q 退出 "
	if m.level <= levelSession {
		right = "Ctrl+C 退出 "
	}
	rows = append(rows, between(style(p.muted).Render(hints), style(p.accent).Render(right), m.width))
	return rows
}

func levelTitle(level navLevel) string {
	switch level {
	case levelProject:
		return "项目"
	case levelSession:
		return "会话"
	case levelDirectory:
		return "目录"
	case levelFile:
		return "改动文件"
	case levelEdit:
		return "修改记录"
	case levelHunk:
		return "差异块"
	}
	return ""
}

func (m *model) navigationCount(level navLevel) int {
	switch level {
	case levelProject:
		return len(m.projects)
	case levelSession:
		return len(m.matchingThreads)
	case levelDirectory:
		return len(m.directories)
	case levelFile:
		return len(m.files)
	case levelEdit:
		if file := m.selectedFile(); file != nil {
			return len(file.Edits)
		}
	case levelHunk:
		return len(m.hunks)
	}
	return 0
}

func (m *model) navigationCursor(level navLevel) int {
	switch level {
	case levelProject:
		return m.projectCursor
	case levelSession:
		return m.pickerCursor
	case levelDirectory:
		return m.directoryCursor
	case levelFile:
		return m.fileCursor
	case levelEdit:
		return m.editCursor
	case levelHunk:
		return m.hunkCursor
	}
	return 0
}

func (m *model) navigationRows(level navLevel, width int) []string {
	p := m.palette()
	focus := " "
	if m.level == level && !m.focusDiff {
		focus = "▸"
	}
	count := m.navigationCount(level)
	unavailable := level >= levelDirectory && m.session.Thread.ID == ""
	if level == levelDirectory && m.level == levelSession && m.selectedThreadID() != m.session.Thread.ID {
		unavailable = true
	}
	if unavailable {
		count = 0
	}
	rows := []string{fit(style(p.accent).Bold(true).Render(fmt.Sprintf("%s %s %d", focus, levelTitle(level), count)), width)}
	hint := "Enter 进入下一层"
	if level == levelSession {
		hint = "输入搜索全部会话"
		if m.pickerQuery != "" {
			hint = "/ " + m.pickerQuery
			if m.level == levelSession && !m.focusDiff {
				hint += "▏"
			}
		}
	} else if level == levelFile {
		hint = "/ 过滤当前目录"
		if m.fileQuery != "" || m.filtering && m.level == levelFile {
			hint = "/ " + m.fileQuery
			if m.filtering && m.level == levelFile {
				hint += "▏"
			}
		}
	}
	rows = append(rows, fit(" "+style(p.muted).Render(hint), width))
	if !unavailable {
		offset, cursor := m.listOffset(level), m.navigationCursor(level)
		for i := offset; i < count && len(rows)+1 < m.bodyHeight(); i++ {
			first, second := m.navigationItem(level, i, width)
			if i == cursor {
				first = style(p.fg).Bold(true).Background(p.selected).Render(first)
				second = style(p.muted).Background(p.selected).Render(second)
			}
			rows = append(rows, first, second)
		}
	}
	if count == 0 {
		message := "没有可显示的项目"
		if m.listLoading || m.loading {
			message = "正在读取…"
		} else if unavailable {
			message = "Enter 读取所选会话"
		} else if level == levelFile && m.fileQuery != "" {
			message = "当前目录没有匹配文件"
		} else if level == levelSession && m.pickerQuery != "" {
			message = "没有匹配的会话"
		}
		rows = append(rows, fit("  "+style(p.muted).Render(message), width))
	}
	for len(rows) < m.bodyHeight() {
		rows = append(rows, strings.Repeat(" ", width))
	}
	return rows[:m.bodyHeight()]
}

func (m *model) navigationItem(level navLevel, index, width int) (string, string) {
	p := m.palette()
	mark := "  "
	if index == m.navigationCursor(level) {
		mark = "› "
	}
	switch level {
	case levelProject:
		item := m.projects[index]
		return between(mark+style(p.fg).Render(clean(item.Name)), style(p.muted).Render(fmt.Sprintf("%d 会话 ", item.Sessions)), width), fit("  "+style(p.muted).Render(clean(item.Path)), width)
	case levelSession:
		thread := m.threads[m.matchingThreads[index]]
		date := time.Unix(thread.UpdatedAt, 0).Local().Format("01-02 15:04") + " "
		second := clean(thread.CWD)
		if thread.Archived {
			second = "[已归档] " + second
		}
		return between(mark+clean(thread.Title()), style(p.muted).Render(date), width), fit("  "+style(p.muted).Render(second), width)
	case levelDirectory:
		item := m.directories[index]
		return between(mark+clean(item.Label), style(p.muted).Render(fmt.Sprintf("%d 文件 ", item.Files)), width), fit("  "+style(p.muted).Render(clean(item.Path)), width)
	case levelFile:
		file := m.session.Files[m.files[index]]
		count := fmt.Sprintf("%d 次 ", len(file.Edits))
		stats := style(p.add).Render(fmt.Sprintf("+%d", file.Added)) + " " + style(p.remove).Render(fmt.Sprintf("−%d", file.Removed)) + " "
		return between(mark+clean(filepath.Base(file.Path)), count, width), between("  "+style(p.muted).Render(clean(file.Path)), stats, width)
	case levelEdit:
		edit := m.selectedFile().Edits[index]
		stamp := "时间未记录"
		if edit.Time != 0 {
			stamp = time.Unix(edit.Time, 0).Local().Format("01-02 15:04")
		}
		stats := style(p.add).Render(fmt.Sprintf("+%d", edit.Added)) + " " + style(p.remove).Render(fmt.Sprintf("−%d", edit.Removed)) + " "
		first := between(mark+stamp+"  "+editKind(edit), stats, width)
		second := fit("  "+style(p.muted).Render(fmt.Sprintf("第 %d 轮 · %s", edit.TurnNumber, clean(edit.Prompt))), width)
		return first, second
	case levelHunk:
		h := m.hunks[index]
		stats := style(p.add).Render(fmt.Sprintf("+%d", h.Added)) + " " + style(p.remove).Render(fmt.Sprintf("−%d", h.Removed)) + " "
		return between(mark+fmt.Sprintf("差异块 %d", index+1), stats, width), fit("  "+style(p.muted).Render(clean(h.Title)), width)
	}
	return "", ""
}

func editKind(edit changes.Edit) string {
	kind := map[string]string{"add": "新建", "delete": "删除", "update": "修改"}[edit.Kind]
	if kind == "" {
		kind = "修改"
	}
	if edit.From != "" {
		kind = "移动"
	}
	return kind
}

func (m *model) diffRows(width int) []string {
	p := m.palette()
	file := m.selectedFile()
	if file == nil {
		rows := []string{fit(" 文件修改记录", width), ""}
		message := "逐层选择文件、修改记录和差异块。"
		if m.loading {
			message = "正在读取文件修改，已发现的文件可以直接查看。"
		} else if len(m.session.Files) == 0 {
			message = "此会话没有保存成功的文件修改记录。通过脚本写文件不一定留下 diff。"
		}
		for _, line := range strings.Split(ansi.Wrap(message, max(1, width-4), " "), "\n") {
			rows = append(rows, fit("  "+style(p.muted).Render(line), width))
		}
		for len(rows) < m.bodyHeight() {
			rows = append(rows, strings.Repeat(" ", width))
		}
		return rows[:m.bodyHeight()]
	}
	focus := " "
	if m.focusDiff {
		focus = "▸"
	}
	name := style(p.fg).Bold(true).Render(focus + " " + clean(filepath.Base(file.Path)))
	stats := fmt.Sprintf("%d 次修改  累计 ", len(file.Edits)) + style(p.add).Render(fmt.Sprintf("+%d", file.Added)) + " " + style(p.remove).Render(fmt.Sprintf("−%d", file.Removed)) + " "
	mode := "连续"
	if !m.continuous {
		mode = "单块"
		if h := m.selectedHunk(); h != nil {
			stats = fmt.Sprintf("差异块 %d/%d  ", m.hunkCursor+1, len(m.hunks)) + style(p.add).Render(fmt.Sprintf("+%d", h.Added)) + " " + style(p.remove).Render(fmt.Sprintf("−%d", h.Removed)) + " "
		}
	}
	if m.opts.Preferences.Split && width >= 60 {
		mode += " · 并排"
	} else {
		mode += " · 单栏"
	}
	if m.opts.Preferences.Wrap {
		mode += " · 换行"
	} else {
		mode += " · 横向滚动"
	}
	subtitle := clean(file.Path)
	if !m.continuous {
		if h := m.selectedHunk(); h != nil {
			subtitle = clean(h.Title)
		}
	}
	rows := []string{between(name, stats, width), between("  "+style(p.muted).Render(subtitle), style(p.muted).Render(mode+" "), width)}
	if !m.continuous && m.selectedHunk() == nil {
		rows = append(rows, fit("  "+style(p.muted).Render("这次修改没有可显示的差异块。"), width))
	}
	for i := m.diffOffset; i < len(m.rows) && len(rows) < m.bodyHeight(); i++ {
		rows = append(rows, cut(m.rows[i].text, width))
	}
	for len(rows) < m.bodyHeight() {
		rows = append(rows, strings.Repeat(" ", width))
	}
	return rows[:m.bodyHeight()]
}

func (m *model) helpRows() []string {
	p := m.palette()
	rows := []string{m.brandRow(), "",
		"  层级导航", "",
		"  项目 → 会话 → 目录 → 文件 → 修改记录 → 差异块；右侧显示所选 diff。",
		"  同屏最多三栏，空间不足时收起左侧层级。",
		"  ← → / h l  返回或进入层级     Enter  展开；在差异中进入阅读",
		"  ↑↓ / j k   选择或滚动         Tab / Shift+Tab  切换可见区域",
		"  鼠标点击选择；滚轮滚动鼠标所在区域。",
		"  输入文字搜索会话；Ctrl+R 刷新会话列表。",
		"  / 或 f     过滤当前目录中的文件；Esc 清除筛选或返回",
		"  c          单块 / 连续时间线    [ / ]  上一条 / 下一条修改",
		"  Space      连续模式中折叠或展开当前修改",
		"  v          单栏 / 并排          w  自动换行 / 横向滚动",
		"  不换行且聚焦差异时，← → 横向滚动；h 返回差异块。",
		"  b          隐藏 / 显示导航      t  跟随终端 / 深色 / 浅色",
		"  y          复制完整路径         r  重新读取当前会话",
		"  q / Ctrl+C 退出", "",
		"  统计口径", "",
		"  汇总执行成功的修改记录；增删行数为历次修改之和。",
		"  目录是按历史路径生成的分组，不读取当前文件系统。",
		"  不依赖 Git。脚本、外部工具等未留下记录的写入无法还原。",
		"  界面只读取历史，不恢复会话、不修改文件。"}
	for _, warning := range m.session.Warnings {
		rows = append(rows, "  "+style(p.remove).Render(clean(warning)))
	}
	rows = append(rows, "", style(p.accent).Render("  Esc / ? 关闭帮助"))
	if len(rows) > m.height {
		rows = append(rows[:m.height-1], style(p.accent).Render("  Esc / ? 关闭 · 完整操作见 docs/usage.md"))
	}
	return rows
}
