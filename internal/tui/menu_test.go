package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Async23/codex-changes/internal/preferences"
	"github.com/charmbracelet/x/ansi"
)

func TestMenuFrameAndChoicesFitEverySupportedSize(t *testing.T) {
	for _, size := range [][2]int{{40, 10}, {40, 24}, {64, 12}, {80, 10}, {80, 24}, {100, 20}, {158, 39}, {220, 38}} {
		for _, theme := range []string{"dark", "light"} {
			for level := levelProject; level <= levelHunk; level++ {
				for _, prefix := range []string{",", "m"} {
					t.Run(fmt.Sprintf("%dx%d/%s/%s/%s", size[0], size[1], theme, levelKey(level), prefix), func(t *testing.T) {
						m := demoModel(t)
						m.width, m.height = size[0], size[1]
						m.level, m.focusDiff, m.listMenu = level, false, prefix
						m.opts.Preferences.Theme = theme
						rows := m.listMenuRows()
						if len(rows) > m.height {
							t.Fatalf("menu requires %d rows in a %d-row terminal", len(rows), m.height)
						}
						for i, row := range rows {
							if width := ansi.StringWidth(row); width != m.width {
								t.Fatalf("row %d has width %d, want %d: %s", i, width, m.width, ansi.Strip(row))
							}
						}
						plain := ansi.Strip(strings.Join(rows, "\n"))
						for _, corner := range []string{"┌", "┐", "└", "┘"} {
							if strings.Count(plain, corner) != 1 {
								t.Fatalf("menu frame is incomplete: %s", plain)
							}
						}
						if strings.TrimSpace(rows[0]) != "" || strings.TrimSpace(rows[len(rows)-1]) != "" {
							t.Fatal("menu lost its separation from the underlying view")
						}
						if !strings.Contains(plain, "Esc 取消") || strings.Contains(plain, "…") || strings.Count(plain, "›") != 1 {
							t.Fatalf("menu clipped a choice, the exit hint or current selection: %s", plain)
						}
					})
				}
			}
		}
	}
}

func TestSortMenuKeepsEveryShortcutVisible(t *testing.T) {
	shortcut := regexp.MustCompile(`\b([a-zA-Z](?:/[A-Z])?) +→`)
	for _, size := range [][2]int{{40, 10}, {64, 12}, {80, 24}, {158, 39}} {
		m := demoModel(t)
		m.width, m.height = size[0], size[1]
		m.level, m.focusDiff, m.listMenu = levelFile, false, ","
		m.opts.Preferences.Lists = map[string]preferences.List{"file": {Sort: "created", Reverse: true}}
		plain := ansi.Strip(strings.Join(m.listMenuRows(), "\n"))
		counts := make(map[string]int)
		for _, match := range shortcut.FindAllStringSubmatch(plain, -1) {
			for _, key := range strings.Split(match[1], "/") {
				counts[key]++
			}
		}
		for _, key := range "daAnNmMbBcCsSeE" {
			if counts[string(key)] != 1 {
				t.Fatalf("%dx%d: key %c is missing or repeated: %s", size[0], size[1], key, plain)
			}
		}
		if size[0] == 158 {
			if strings.Contains(plain, "b/B") || !strings.Contains(plain, "› B → 首次修改时间 ↓") {
				t.Fatalf("wide menu did not distinguish ascending and descending choices: %s", plain)
			}
			multiple := false
			for _, line := range strings.Split(plain, "\n") {
				multiple = multiple || strings.Count(line, "→") >= 3
			}
			if !multiple {
				t.Fatal("wide menu still stacks every option in one column")
			}
		}
		if size == [2]int{40, 10} && !strings.Contains(plain, "↓ b/B → 首次修改") {
			t.Fatalf("compact menu lost the selected sort direction: %s", plain)
		}
	}
}

func TestClosingMenuRestoresUnderlyingFrame(t *testing.T) {
	for _, theme := range []string{"dark", "light"} {
		m := demoModel(t)
		m.level, m.focusDiff = levelFile, false
		m.opts.Preferences.Theme = theme
		m.rebuildDiff()
		before := m.render()
		for _, key := range []rune{',', 'm'} {
			typeKey(m, key)
			if m.render() == before {
				t.Fatal("menu did not appear")
			}
			m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
			if m.render() != before {
				t.Fatal("closing the menu changed the underlying frame")
			}
		}
	}
}
