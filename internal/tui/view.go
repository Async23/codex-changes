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
	"github.com/charmbracelet/x/ansi"
)

type palette struct {
	bg, fg, muted, border, accent, selected, add, remove, addBG, removeBG color.Color
}

func (m *model) palette() palette {
	dark := m.dark
	if m.opts.Preferences.Theme != "auto" {
		dark = m.opts.Preferences.Theme == "dark"
	}
	c := lipgloss.Color
	if dark {
		return palette{c("#171a1d"), c("#e5e9eb"), c("#9ba5ac"), c("#414950"), c("#84cfc2"), c("#293c3c"), c("#9ad9ab"), c("#f0a3a3"), c("#20352b"), c("#3b292c")}
	}
	return palette{c("#f6f5f1"), c("#293238"), c("#647178"), c("#c8cecc"), c("#087568"), c("#dcece7"), c("#176537"), c("#a12d36"), c("#e2efe5"), c("#f7e4e5")}
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
	} else if m.picker {
		rows = m.pickerRows()
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
	return between(left, style(p.muted).Render("s 切换会话  ? 帮助 "), m.width)
}

func (m *model) mainRows() []string {
	p := m.palette()
	title := "正在读取会话…"
	if m.session.Thread.ID != "" {
		title = clean(m.session.Thread.Title())
	}
	second := " " + style(p.fg).Bold(true).Render(title) + "  " + style(p.muted).Render(clean(m.session.Thread.CWD))
	rows := []string{m.brandRow(), fit(second, m.width), style(p.border).Render(strings.Repeat("─", m.width))}
	if m.narrow() && !m.focusDiff && !m.hideFiles {
		rows = append(rows, m.fileRows(m.width)...)
	} else if left := m.sidebarWidth(); left > 0 {
		files, diff := m.fileRows(left), m.diffRows(m.diffWidth())
		for i := 0; i < m.bodyHeight(); i++ {
			rows = append(rows, files[i]+style(p.border).Render("│")+diff[i])
		}
	} else {
		rows = append(rows, m.diffRows(m.width)...)
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
	hints := " ↑↓ 移动  Tab 区域  / 筛文件  s 会话  [ ] 修改  v 并排  w 换行  t 主题"
	if m.narrow() {
		hints = " ↑↓ 移动  Enter 查看  Tab 返回  / 筛选  s 会话"
	}
	rows = append(rows, between(style(p.muted).Render(hints), style(p.accent).Render("? 帮助  q 退出 "), m.width))
	return rows
}

func (m *model) fileRows(width int) []string {
	p := m.palette()
	focus := " "
	if !m.focusDiff {
		focus = "▸"
	}
	rows := []string{fit(style(p.accent).Bold(true).Render(fmt.Sprintf("%s 改动文件 %d", focus, len(m.session.Files))), width)}
	query := "/ 过滤文件名或路径"
	if m.fileQuery != "" || m.filtering {
		query = "/ " + m.fileQuery
		if m.filtering {
			query += "▏"
		}
	}
	rows = append(rows, fit(" "+style(p.muted).Render(query), width))
	for i := m.fileOffset; i < len(m.files) && len(rows)+1 < m.bodyHeight(); i++ {
		file := m.session.Files[m.files[i]]
		selected := i == m.fileCursor
		mark := " "
		if selected {
			mark = "›"
		}
		name := mark + " " + clean(filepath.Base(file.Path))
		count := fmt.Sprintf(" %d 次 ", len(file.Edits))
		first := between(name, count, width)
		parent := filepath.Dir(file.Path)
		if rel, err := filepath.Rel(m.session.Thread.CWD, parent); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			parent = rel
		}
		if parent == m.session.Thread.CWD {
			parent = "."
		}
		stats := style(p.add).Render(fmt.Sprintf("+%d", file.Added)) + " " + style(p.remove).Render(fmt.Sprintf("−%d", file.Removed)) + " "
		second := between("  "+style(p.muted).Render(clean(parent)), stats, width)
		if selected {
			first = style(p.fg).Bold(true).Background(p.selected).Render(first)
			second = style(p.muted).Background(p.selected).Render(second)
		}
		rows = append(rows, first, second)
	}
	if len(m.files) == 0 && !m.loading {
		rows = append(rows, fit("  没有匹配的文件", width))
	}
	for len(rows) < m.bodyHeight() {
		rows = append(rows, strings.Repeat(" ", width))
	}
	return rows[:m.bodyHeight()]
}

func (m *model) diffRows(width int) []string {
	p := m.palette()
	file := m.selectedFile()
	if file == nil {
		rows := []string{fit("  文件修改记录", width), ""}
		message := "选择左侧文件，查看每一次修改。"
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
	mode := "单栏"
	if m.opts.Preferences.Split && width >= 60 {
		mode = "并排"
	}
	if m.opts.Preferences.Wrap {
		mode += " · 换行"
	} else {
		mode += " · 横向滚动"
	}
	rows := []string{between(name, stats, width), between("  "+style(p.muted).Render(clean(file.Path)), style(p.muted).Render(mode+" "), width)}
	for i := m.diffOffset; i < len(m.rows) && len(rows) < m.bodyHeight(); i++ {
		rows = append(rows, cut(m.rows[i].text, width))
	}
	for len(rows) < m.bodyHeight() {
		rows = append(rows, strings.Repeat(" ", width))
	}
	return rows[:m.bodyHeight()]
}

func (m *model) pickerRows() []string {
	p := m.palette()
	query := m.pickerQuery + "▏"
	if m.pickerQuery == "" {
		query = "输入会话名或目录搜索… ▏"
	}
	rows := []string{m.brandRow(), " " + style(p.fg).Bold(true).Render("选择会话"), fit(" / "+style(p.accent).Render(query), m.width), style(p.border).Render(strings.Repeat("─", m.width))}
	for i := m.pickerOffset; i < len(m.matchingThreads) && len(rows)+1 < m.height-3; i++ {
		thread := m.threads[m.matchingThreads[i]]
		mark := " "
		if i == m.pickerCursor {
			mark = "›"
		}
		name := mark + " " + clean(thread.Title())
		if thread.Archived {
			name += " [已归档]"
		}
		date := time.Unix(thread.UpdatedAt, 0).Local().Format("01-02 15:04") + " "
		first := between(name, style(p.muted).Render(date), m.width)
		second := fit("  "+style(p.muted).Render(clean(thread.CWD)), m.width)
		if i == m.pickerCursor {
			first = style(p.fg).Background(p.selected).Bold(true).Render(first)
			second = style(p.muted).Background(p.selected).Render(second)
		}
		rows = append(rows, first, second)
	}
	if len(m.matchingThreads) == 0 && !m.listLoading {
		rows = append(rows, "  没有找到会话。试试搜索目录，或按 Ctrl+R 刷新。")
	}
	for len(rows) < m.height-3 {
		rows = append(rows, "")
	}
	status := fmt.Sprintf(" %d 个匹配 · %d 个会话", len(m.matchingThreads), len(m.threads))
	if m.listLoading {
		status = " 正在读取会话列表…"
	}
	if m.err != "" {
		status = " " + clean(m.err)
	}
	return append(rows, style(p.muted).Render(status), "", between(" ↑↓ 选择  Enter 打开  鼠标点选  Ctrl+R 刷新", "Esc 返回  Ctrl+C 退出 ", m.width))
}

func (m *model) helpRows() []string {
	p := m.palette()
	rows := []string{m.brandRow(), "", "  操作", "", "  鼠标点击文件；滚轮滚动鼠标所在区域。点击修改标题可折叠。", "  s         选择会话：直接输入名称或目录，再按 Enter", "  /         按文件名或路径过滤；Esc 清除筛选", "  Tab       切换文件列表和差异；Enter 打开文件", "  ↑↓ / j k  移动或滚动     PgUp / PgDn  翻页", "  g / G     到开头或末尾    [ / ]  上一次或下一次修改", "  Space     折叠或展开当前修改", "  v         单栏 / 并排     w  自动换行 / 横向滚动", "  ←→ / h l  不换行时横向滚动", "  b         隐藏 / 显示文件列表", "  t         跟随终端 / 深色 / 浅色", "  y         复制完整路径（macOS 原生；其他终端用 OSC 52）", "  r         重新读取当前会话", "  q / Ctrl+C 退出", "", "  统计口径", "  汇总执行成功的修改记录；增删行数为历次修改之和。", "  不依赖 Git。脚本、外部工具等未留下记录的写入无法还原。", "  界面只读取历史，不恢复会话、不修改文件。"}
	for _, warning := range m.session.Warnings {
		rows = append(rows, "  "+style(p.remove).Render(clean(warning)))
	}
	rows = append(rows, "", style(p.accent).Render("  Esc / ? 关闭帮助"))
	if len(rows) > m.height {
		rows = append(rows[:m.height-1], style(p.accent).Render("  Esc / ? 关闭 · 完整操作见 docs/usage.md"))
	}
	return rows
}
