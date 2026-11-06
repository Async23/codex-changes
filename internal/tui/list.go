package tui

import (
	"cmp"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Async23/codex-changes/internal/changes"
	"github.com/Async23/codex-changes/internal/preferences"
)

type listOption struct {
	key, value, label string
}

func levelKey(level navLevel) string {
	return [...]string{"project", "session", "directory", "file", "edit", "hunk"}[level]
}

func sortOptions(level navLevel) []listOption {
	options := []listOption{{"d", "", "默认顺序"}}
	if level <= levelFile {
		options = append(options, listOption{"a", "name", "名称"}, listOption{"n", "natural", "自然名称（2 在 10 前）"})
	}
	if level <= levelEdit {
		label := "修改时间"
		if level <= levelSession {
			label = "更新时间"
		}
		options = append(options, listOption{"m", "updated", label})
	}
	if level <= levelFile {
		label := "首次修改时间"
		if level <= levelSession {
			label = "创建时间"
		}
		options = append(options, listOption{"b", "created", label})
	}
	switch level {
	case levelProject:
		options = append(options, listOption{"c", "count", "会话数"})
	case levelDirectory:
		options = append(options, listOption{"c", "count", "文件数"})
	case levelFile:
		options = append(options, listOption{"c", "count", "修改次数"})
	}
	if level >= levelDirectory {
		options = append(options, listOption{"s", "changes", "变更行数"})
	}
	if level == levelFile {
		options = append(options, listOption{"e", "extension", "扩展名"})
	}
	return options
}

func linemodeOptions(level navLevel) []listOption {
	options := []listOption{{"a", "", "默认信息"}, {"n", "none", "隐藏行信息"}}
	for _, option := range sortOptions(level) {
		switch option.value {
		case "updated", "created", "count", "changes":
			options = append(options, option)
		}
	}
	return options
}

func optionFor(options []listOption, value string) (listOption, bool) {
	for _, option := range options {
		if option.value == value {
			return option, true
		}
	}
	return listOption{}, false
}

func (m *model) listPreference(level navLevel) preferences.List {
	pref := m.opts.Preferences.Lists[levelKey(level)]
	if _, ok := optionFor(sortOptions(level), pref.Sort); !ok {
		pref.Sort, pref.Reverse = "", false
	}
	if pref.Sort == "" {
		pref.Reverse = false
	}
	if _, ok := optionFor(linemodeOptions(level), pref.Linemode); !ok {
		pref.Linemode = ""
	}
	return pref
}

func (m *model) listMenuKey(key string) {
	menu := m.listMenu
	if key == "esc" || key == "q" {
		m.listMenu = ""
		return
	}
	options := sortOptions(m.level)
	if menu == "m" {
		options = linemodeOptions(m.level)
	}
	for _, option := range options {
		reverse := menu == "," && option.value != "" && key == strings.ToUpper(option.key)
		if key != option.key && !reverse {
			continue
		}
		pref := m.listPreference(m.level)
		if menu == "," {
			pref.Sort, pref.Reverse = option.value, reverse
			switch option.value {
			case "updated", "created", "count", "changes":
				pref.Linemode = option.value
			}
		} else {
			pref.Linemode = option.value
		}
		if m.opts.Preferences.Lists == nil {
			m.opts.Preferences.Lists = make(map[string]preferences.List)
		}
		m.opts.Preferences.Lists[levelKey(m.level)] = pref
		m.listMenu = ""
		if menu == "," {
			// Reorder the filtered indices, keeping the selected object's identity
			// and its children. Sorting never reloads a session.
			switch m.level {
			case levelProject:
				m.filterProjects("")
			case levelSession:
				m.filterThreads()
			case levelDirectory:
				m.filterDirectories("")
			case levelFile:
				m.filterFiles("")
			case levelEdit:
				m.filterEdits()
			case levelHunk:
				m.filterHunks("")
			}
		}
		m.rebuildDiff()
		return
	}
}

func (m *model) listMenuRows() []string {
	p := m.palette()
	pref := m.listPreference(m.level)
	options, selected := sortOptions(m.level), pref.Sort
	title, hint := "排序", "小写升序 / 大写降序 · Esc 取消"
	if m.listMenu == "m" {
		options, selected = linemodeOptions(m.level), pref.Linemode
		title, hint = "行信息", "按对应字母选择 · Esc 取消"
	}
	rows := []string{style(p.accent).Bold(true).Background(p.selected).Render(fit(" "+title+" · "+levelTitle(m.level), m.width))}
	for _, option := range options {
		key := option.key
		if m.listMenu == "," && option.value != "" {
			key += "/" + strings.ToUpper(key)
		}
		mark := " "
		if option.value == selected {
			mark = "›"
		}
		label := option.label
		if m.listMenu == "," && option.value == selected && selected != "" {
			if pref.Reverse {
				label += " ↓"
			} else {
				label += " ↑"
			}
		}
		rows = append(rows, fmt.Sprintf(" %s %-3s  %s", mark, key, label))
	}
	return append(rows, style(p.muted).Render(" "+hint))
}

// A listEntry describes history metadata only; no current filesystem reads.
// Rendering, width measurement and sorting all use the same fields.
type listEntry struct {
	id, name, extension string
	count               int
	countUnit           string
	added, removed      int
	created, updated    int64
}

func firstTime(a, b int64) int64 {
	if a == 0 || b != 0 && b < a {
		return b
	}
	return a
}

func fileTimes(file changes.File) (first, last int64) {
	for _, edit := range file.Edits {
		first = firstTime(first, edit.Time)
		last = max(last, edit.Time)
	}
	return
}

func hunkIdentity(h *editHunk) string { return fmt.Sprintf("%d:%d", h.Start, h.End) }

func (m *model) listEntry(level navLevel, index int) listEntry {
	switch level {
	case levelProject:
		item := m.projects[index]
		return listEntry{id: item.Path, name: clean(item.Name), count: item.Sessions, countUnit: "会话", created: item.CreatedAt, updated: item.UpdatedAt}
	case levelSession:
		item := m.threads[index]
		name := clean(item.Title())
		if item.Archived {
			name += " [已归档]"
		}
		return listEntry{id: item.ID, name: name, created: item.CreatedAt, updated: item.UpdatedAt}
	case levelDirectory:
		item := m.directories[index]
		return listEntry{id: item.Path, name: clean(item.Label), count: item.Files, countUnit: "文件", added: item.Added, removed: item.Removed, created: item.CreatedAt, updated: item.UpdatedAt}
	case levelFile:
		item := m.session.Files[index]
		first, last := fileTimes(item)
		return listEntry{id: item.Path, name: clean(filepath.Base(item.Path)), extension: strings.ToLower(filepath.Ext(item.Path)), count: len(item.Edits), countUnit: "次", added: item.Added, removed: item.Removed, created: first, updated: last}
	case levelEdit:
		item := m.selectedFile().Edits[index]
		return listEntry{id: item.ID, name: fmt.Sprintf("%s · 第 %d 轮", editKind(item), item.TurnNumber), added: item.Added, removed: item.Removed, updated: item.Time}
	case levelHunk:
		item := m.hunks[index]
		return listEntry{id: hunkIdentity(&item), name: fmt.Sprintf("差异块 %d", index+1), added: item.Added, removed: item.Removed}
	}
	return listEntry{}
}

func (m *model) navigationIndices(level navLevel) []int {
	switch level {
	case levelProject:
		return m.matchingProjects
	case levelSession:
		return m.matchingThreads
	case levelDirectory:
		return m.matchingDirectories
	case levelFile:
		return m.files
	case levelEdit:
		return m.matchingEdits
	case levelHunk:
		return m.matchingHunks
	}
	return nil
}

func (m *model) sortMatches(level navLevel, matches []int) {
	pref := m.listPreference(level)
	if pref.Sort == "" {
		if level > levelSession {
			return
		}
		pref.Sort, pref.Reverse = "updated", true
	}
	// Compute metadata once per item, rather than for every comparison.
	entries := make(map[int]listEntry, len(matches))
	for _, index := range matches {
		entries[index] = m.listEntry(level, index)
	}
	sort.SliceStable(matches, func(i, j int) bool {
		a, b := entries[matches[i]], entries[matches[j]]
		order := 0
		switch pref.Sort {
		case "name":
			order = strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name))
		case "natural":
			order = naturalCompare(strings.ToLower(a.name), strings.ToLower(b.name))
		case "extension":
			order = strings.Compare(a.extension, b.extension)
		case "count":
			order = cmp.Compare(a.count, b.count)
		case "changes":
			order = cmp.Compare(a.added+a.removed, b.added+b.removed)
		case "updated", "created":
			x, y := a.updated, b.updated
			if pref.Sort == "created" {
				x, y = a.created, b.created
			}
			if (x == 0) != (y == 0) {
				return y == 0 // Unknown timestamps stay last in both directions.
			}
			order = cmp.Compare(x, y)
		}
		if order == 0 {
			return matches[i] < matches[j]
		}
		if pref.Reverse {
			return order > 0
		}
		return order < 0
	})
}

func naturalCompare(a, b string) int {
	originalA, originalB := a, b
	for len(a) > 0 && len(b) > 0 {
		if a[0] >= '0' && a[0] <= '9' && b[0] >= '0' && b[0] <= '9' {
			i, j := 0, 0
			for i < len(a) && a[i] >= '0' && a[i] <= '9' {
				i++
			}
			for j < len(b) && b[j] >= '0' && b[j] <= '9' {
				j++
			}
			x, y := strings.TrimLeft(a[:i], "0"), strings.TrimLeft(b[:j], "0")
			if order := cmp.Compare(len(x), len(y)); order != 0 {
				return order
			}
			if order := strings.Compare(x, y); order != 0 {
				return order
			}
			a, b = a[i:], b[j:]
			continue
		}
		if order := cmp.Compare(a[0], b[0]); order != 0 {
			return order
		}
		a, b = a[1:], b[1:]
	}
	if order := cmp.Compare(len(a), len(b)); order != 0 {
		return order
	}
	return strings.Compare(originalA, originalB)
}

func listTime(stamp int64) string {
	if stamp == 0 {
		return "时间未记录"
	}
	return time.Unix(stamp, 0).Local().Format("01-02 15:04")
}

func (m *model) navigationParts(level navLevel, index int) (string, string) {
	item := m.listEntry(level, m.navigationIndices(level)[index])
	p := m.palette()
	count := fmt.Sprintf("%d %s", item.count, item.countUnit)
	stats := style(p.add).Render(fmt.Sprintf("+%d", item.added)) + " " + style(p.remove).Render(fmt.Sprintf("−%d", item.removed))
	var info string
	switch m.listPreference(level).Linemode {
	case "none":
	case "updated":
		info = listTime(item.updated)
	case "created":
		info = listTime(item.created)
	case "count":
		info = count
	case "changes":
		info = stats
	default:
		switch level {
		case levelProject, levelDirectory:
			info = count
		case levelSession:
			info = listTime(item.updated)
		case levelFile:
			info = count + "  " + stats
		case levelEdit:
			info = listTime(item.updated) + "  " + stats
		case levelHunk:
			info = stats
		}
	}
	if info != "" {
		info = style(p.muted).Render(info)
	}
	return item.name, info
}

func (m *model) listHeading(level navLevel) string {
	heading := fmt.Sprintf("%s %d", levelTitle(level), m.navigationCount(level))
	pref := m.listPreference(level)
	if pref.Sort != "" {
		option, _ := optionFor(sortOptions(level), pref.Sort)
		label := option.label
		if pref.Sort == "natural" {
			label = "自然名称"
		}
		direction := "↑"
		if pref.Reverse {
			direction = "↓"
		}
		heading += " · " + label + direction
	}
	return heading
}
