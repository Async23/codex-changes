package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type menuChoice struct {
	key, label, mark string
}

func (m *model) menuChoices(compact bool) []menuChoice {
	pref := m.listPreference(m.level)
	options, selected := sortOptions(m.level), pref.Sort
	if m.listMenu == "m" {
		options, selected = linemodeOptions(m.level), pref.Linemode
	}
	var choices []menuChoice
	for _, option := range options {
		label := option.label
		if option.value == "natural" {
			label = "自然名称"
		}
		if compact {
			switch label {
			case "首次修改时间":
				label = "首次修改"
			case "隐藏行信息":
				label = "隐藏信息"
			}
		}
		choice := menuChoice{key: option.key, label: label}
		if option.value == selected {
			choice.mark = "›"
		}
		if m.listMenu != "," || option.value == "" {
			choices = append(choices, choice)
			continue
		}
		if compact {
			choice.key += "/" + strings.ToUpper(option.key)
			if choice.mark != "" {
				choice.mark = "↑"
				if pref.Reverse {
					choice.mark = "↓"
				}
			}
			choices = append(choices, choice)
			continue
		}
		ascending, descending := choice, choice
		ascending.label += " ↑"
		descending.key = strings.ToUpper(option.key)
		descending.label += " ↓"
		if pref.Reverse {
			ascending.mark = ""
		} else {
			descending.mark = ""
		}
		choices = append(choices, ascending, descending)
	}
	return choices
}

type menuLayout struct {
	choices                       []menuChoice
	columns, rows, keyWidth       int
	padding, verticalPadding, gap int
	contentWidth                  int
}

func (m *model) menuLayout(compact bool) menuLayout {
	l := menuLayout{choices: m.menuChoices(compact), padding: 2, verticalPadding: 1, gap: 4}
	minCell, maxColumns := 30, 3
	if compact {
		l.padding, l.gap = 1, 2
		minCell, maxColumns = 16, 2
	}
	// Two outer margins, two border cells, then equal inner padding.
	l.contentWidth = m.width - 4 - 2*l.padding
	for _, choice := range l.choices {
		l.keyWidth = max(l.keyWidth, ansi.StringWidth(choice.key))
	}
	cellWidth := minCell
	for _, choice := range l.choices {
		cellWidth = max(cellWidth, 2+l.keyWidth+3+ansi.StringWidth(choice.label))
	}
	l.columns = max(1, min(maxColumns, (l.contentWidth+l.gap)/(cellWidth+l.gap)))
	l.rows = (len(l.choices) + l.columns - 1) / l.columns
	// Keep every shortcut visible before sacrificing the panel's margins.
	if l.rows+6 > m.height {
		l.verticalPadding = 0
	}
	return l
}

func (m *model) listMenuRows() []string {
	p := m.palette()
	l := m.menuLayout(false)
	if l.rows+4+2*l.verticalPadding > m.height {
		l = m.menuLayout(true)
	}
	title, hint := "排序", "小写↑ 大写↓ · Esc 取消"
	if m.listMenu == "m" {
		title, hint = "行信息", "按键选择 · Esc 取消"
	}
	title += " · " + levelTitle(m.level)
	border := style(p.accent).Background(p.menuBG)
	panel := style(p.menuDesc).Background(p.menuBG)
	blank := strings.Repeat(" ", m.width)
	frame := func(left, right, label string, bold bool) string {
		label = " " + label + " "
		label = ansi.Truncate(label, m.width-7, "…")
		text := border.Bold(bold).Render(label)
		return " " + border.Render(left+"─") + text + border.Render(strings.Repeat("─", m.width-5-ansi.StringWidth(label))+right) + " "
	}
	row := func(content string) string {
		padding := strings.Repeat(" ", l.padding)
		trailing := strings.Repeat(" ", max(0, l.contentWidth-ansi.StringWidth(content)))
		return " " + border.Render("│") + panel.Render(padding) + content + panel.Render(trailing+padding) + border.Render("│") + " "
	}
	rows := []string{blank, frame("┌", "┐", title, true)}
	if l.verticalPadding > 0 {
		rows = append(rows, row(""))
	}
	cellWidth := (l.contentWidth - (l.columns-1)*l.gap) / l.columns
	for r := 0; r < l.rows; r++ {
		var line strings.Builder
		for c := 0; c < l.columns; c++ {
			if c > 0 {
				line.WriteString(panel.Render(strings.Repeat(" ", l.gap)))
			}
			index := r*l.columns + c
			if index >= len(l.choices) {
				line.WriteString(panel.Render(strings.Repeat(" ", cellWidth)))
				continue
			}
			choice := l.choices[index]
			bg, desc := p.menuBG, p.menuDesc
			if choice.mark != "" {
				bg, desc = p.selected, p.fg
			}
			key := style(p.accent).Background(bg).Bold(true)
			label := style(desc).Background(bg).Bold(choice.mark != "")
			text := key.Render(fit(choice.mark, 1)+" "+fit(choice.key, l.keyWidth)) +
				style(p.muted).Background(bg).Render(" → ") + label.Render(fit(choice.label, cellWidth-l.keyWidth-5))
			line.WriteString(text)
		}
		rows = append(rows, row(line.String()))
	}
	if l.verticalPadding > 0 {
		rows = append(rows, row(""))
	}
	return append(rows, frame("└", "┘", hint, false), blank)
}
