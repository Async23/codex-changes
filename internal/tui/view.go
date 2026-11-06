package tui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Async23/codex-changes/internal/changes"
	"github.com/Async23/codex-changes/internal/highlight"
	"github.com/charmbracelet/x/ansi"
)

type palette struct {
	bg, fg, muted, border, accent, selected, add, remove, addBG, removeBG color.Color
	addWord, removeWord                                                   color.Color
	menuBG, menuDesc                                                      color.Color
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
			menuBG: c("#242a30"), menuDesc: c("#e6c384"),
			add: c("#72ce88"), remove: c("#e88383"), addBG: c("#123019"), removeBG: c("#401b20"), addWord: c("#245c30"), removeWord: c("#753038"),
			syntax: [highlight.RoleCount]color.Color{c("#f8f8f2"), c("#f92672"), c("#66d9ef"), c("#e6db74"), c("#be84ff"), c("#a0a194"), c("#a6e22e"), c("#a6e22e"), c("#f92672"), c("#f92672"), c("#66d9ef")},
		}
	}
	return palette{
		bg: c("#faf4ed"), fg: c("#333333"), muted: c("#7c817a"), border: c("#d6d1c9"), accent: c("#087568"), selected: c("#e3eee7"),
		menuBG: c("#fffaf3"), menuDesc: c("#9a6700"),
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
	if right == "" || ansi.StringWidth(right)+4 >= width {
		return fit(left, width)
	}
	return fit(left, width-ansi.StringWidth(right)-1) + " " + right
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
	if m.listMenu != "" {
		menu := m.listMenuRows()
		start := max(0, h-len(menu))
		rows = append(rows[:min(start, len(rows))], menu...)
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
	return fit(left, m.width)
}

type column struct {
	level navLevel
	diff  bool
	width int
}

func navColumn(level navLevel) column { return column{level: level} }
func diffColumn() column              { return column{diff: true} }

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
	return m.fitColumns(columns)
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
	if m.level == levelProject {
		if p := m.selectedProject(); p != nil {
			title = clean(p.Path)
		}
	} else if m.level == levelSession {
		if i := m.selectedThreadIndex(); i >= 0 {
			thread := m.threads[i]
			title = clean(thread.CWD) + " › " + clean(thread.Title())
		}
	} else if m.session.Thread.ID != "" {
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
	hints := " j/k 选择  h/l 层级  / 筛选  , 排序  m 信息  J/K 预览  " + toggle
	if m.level <= levelSession {
		hints = " j/k 选择  h/l 层级  / 筛选  , 排序  m 信息  Enter 打开"
	} else if m.focusDiff {
		hints = " j/k 滚动  h 返回  [ ] 修改  " + toggle + "  v 并排  w 换行"
	}
	if m.narrow() {
		hints = " j/k 移动  h/l 层级  , 排序  m 信息"
		if m.focusDiff {
			hints = " j/k 滚动  h 返回  " + toggle
		}
	}
	rows = append(rows, between(style(p.muted).Render(hints), style(p.accent).Render("? 帮助  q 退出 "), m.width))
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
		return len(m.matchingProjects)
	case levelSession:
		return len(m.matchingThreads)
	case levelDirectory:
		return len(m.matchingDirectories)
	case levelFile:
		return len(m.files)
	case levelEdit:
		return len(m.matchingEdits)
	case levelHunk:
		return len(m.matchingHunks)
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
	focused := m.level == level && !m.focusDiff
	focus := " "
	if focused {
		focus = "▸"
	}
	count := m.navigationCount(level)
	unavailable := m.listUnavailable(level)
	if unavailable {
		count = 0
	}
	heading := m.listHeading(level)
	if unavailable {
		heading = levelTitle(level) + " 0"
	}
	header := style(p.muted).Render(focus + " " + heading)
	if focused {
		header = style(p.accent).Bold(true).Render(focus + " " + heading)
	}
	rows := []string{fit(header, width)}
	hint := ""
	if query := m.currentQueryFor(level); query != "" || m.filtering && m.level == level {
		hint = "/ " + query
		if m.filtering && m.level == level {
			hint += "▏"
		}
	}
	rows = append(rows, fit(" "+style(p.muted).Render(hint), width))
	if !unavailable {
		offset, cursor := m.listOffset(level), m.navigationCursor(level)
		for i := offset; i < count && len(rows) < m.bodyHeight(); i++ {
			row := m.navigationItem(level, i, width)
			if i == cursor {
				if focused {
					row = style(p.fg).Bold(true).Background(p.selected).Render(row)
				} else {
					row = style(p.fg).Bold(true).Render(row)
				}
			}
			rows = append(rows, row)
		}
	}
	if count == 0 {
		message := m.emptyListMessage(level)
		for _, line := range strings.Split(ansi.Hardwrap(message, max(1, width-4), true), "\n") {
			rows = append(rows, fit("  "+style(p.muted).Render(line), width))
		}
	}
	for len(rows) < m.bodyHeight() {
		rows = append(rows, strings.Repeat(" ", width))
	}
	return rows[:m.bodyHeight()]
}

func (m *model) navigationItem(level navLevel, index, width int) string {
	mark := "  "
	if index == m.navigationCursor(level) {
		mark = "› "
	}
	name, info := m.navigationParts(level, index)
	return between(mark+name, info, max(0, width-1)) + strings.Repeat(" ", min(1, width))
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
			stats = fmt.Sprintf("差异块 %d/%d  ", m.matchingHunks[m.hunkCursor]+1, len(m.hunks)) + style(p.add).Render(fmt.Sprintf("+%d", h.Added)) + " " + style(p.remove).Render(fmt.Sprintf("−%d", h.Removed)) + " "
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
		"  列宽按可见名称和行信息调整；空间不足时收起左侧层级。",
		"  ← → / h l  返回或进入层级     Enter  展开；在差异中进入阅读",
		"  ↑↓ / j k   选择或滚动         Tab / Shift+Tab  切换可见区域",
		"  鼠标点击选择；滚轮滚动鼠标所在区域。",
		"  / 或 f     筛选当前栏；Enter 接受，Esc 清除；Ctrl+R 刷新列表",
		"  ,          当前栏排序；小写升序、大写降序，d 恢复默认",
		"  m          切换行信息；a 默认、n 隐藏；菜单按当前层级提供选项",
		"  c          单块 / 连续时间线    [ / ]  上一条 / 下一条修改",
		"  Space      连续模式中折叠或展开当前修改",
		"  v          单栏 / 并排          w  自动换行 / 横向滚动",
		"  J / K      滚动右侧 diff 预览，无需切换焦点",
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
