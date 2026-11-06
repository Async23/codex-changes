package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Async23/codex-changes/internal/changes"
	"github.com/Async23/codex-changes/internal/codex"
	"github.com/Async23/codex-changes/internal/preferences"
	"github.com/charmbracelet/x/ansi"
)

func TestPreviewContentDoesNotResizeBrowsingColumns(t *testing.T) {
	// Measured in Yazi 26.9.1 with the local mobile-auto-layout plugin:
	// 90 -> 52/37/1, 100 -> 52/47/1, 158 -> 52/80/26, 220 -> 52/80/88.
	// Our frame has two separate border cells, leaving two fewer content cells.
	for _, tc := range []struct {
		width int
		want  [3]int
	}{{90, [3]int{52, 35, 1}}, {100, [3]int{52, 45, 1}}, {158, [3]int{52, 80, 24}}, {220, [3]int{52, 80, 86}}} {
		t.Run(fmt.Sprint(tc.width), func(t *testing.T) {
			m := demoModel(t)
			m.width, m.level, m.focusDiff = tc.width, levelSession, false
			m.projects = []project{{Name: strings.Repeat("p", 41), Sessions: 1}}
			m.matchingProjects, m.projectCursor, m.projectOffset = []int{0}, 0, 0
			m.threads = []codex.Thread{{ID: "synthetic-width", Name: strings.Repeat("s", 90)}}
			m.matchingThreads, m.pickerCursor, m.pickerOffset = []int{0}, 0, 0
			m.opts.Preferences.Lists = map[string]preferences.List{"session": {Linemode: "none"}}
			m.session = changes.Session{Thread: m.threads[0]}
			m.directoryCursor, m.directoryOffset = 0, 0
			m.directories, m.matchingDirectories = nil, nil
			before := m.columns()
			if len(before) != len(tc.want) {
				t.Fatalf("unexpected column count: %+v", before)
			}
			for i, want := range tc.want {
				if before[i].width != want {
					t.Fatalf("widths differ from the measured Yazi allocation: %+v, want %v", before, tc.want)
				}
			}
			for _, name := range []string{"short", strings.Repeat("long-directory-", 12), ""} {
				m.directories, m.matchingDirectories = nil, nil
				if name != "" {
					m.directories = []directory{{Label: name, Files: 1}}
					m.matchingDirectories = []int{0}
				}
				after := m.columns()
				if len(after) != 3 {
					t.Fatalf("preview change removed a browsing column: %+v", after)
				}
				for i := range before {
					if after[i].width != before[i].width {
						t.Fatalf("preview change resized unchanged browsing content: %+v -> %+v", before, after)
					}
				}
			}
			m.session.Thread.ID = ""
			for i, c := range m.columns() {
				if c.width != tc.want[i] {
					t.Fatalf("unavailable preview changed column widths: %+v", m.columns())
				}
			}
		})
	}
}

func TestContentWidthsFitLongNamesAndLeaveSpaceForEmptyPreview(t *testing.T) {
	m := demoModel(t)
	m.width = 200
	m.level, m.focusDiff = levelSession, false
	name := "中文-project-with-a-long-descriptive-name"
	m.projects[m.matchingProjects[0]].Name = name
	m.session = changes.Session{Thread: codex.Thread{ID: m.selectedThreadID()}}
	m.directories, m.matchingDirectories = nil, nil
	m.files, m.matchingEdits, m.matchingHunks, m.hunks = nil, nil, nil, nil
	columns := m.columns()
	if len(columns) != 3 || columns[0].width <= 30 || columns[1].width > 40 || columns[2].width <= columns[1].width {
		t.Fatalf("long parent or compact current list does not leave room for the preview: %+v", columns)
	}
	row := ansi.Strip(m.navigationItem(levelProject, 0, columns[0].width))
	if !strings.Contains(row, name) || strings.Contains(row, "…") || !strings.Contains(row, " 会话") {
		t.Fatalf("project name or metadata was clipped despite available space: %q", row)
	}
	m.level = levelProject
	chooseListOption(m, 'm', 'n')
	m.level = levelSession
	if hidden := m.columns()[0].width; hidden >= columns[0].width {
		t.Fatalf("hiding metadata did not reclaim width: %d >= %d", hidden, columns[0].width)
	}
}

func TestColumnWidthFollowsVisibleRows(t *testing.T) {
	m := demoModel(t)
	m.width, m.height = 200, 10
	m.level, m.focusDiff = levelSession, false
	m.projects = nil
	m.matchingProjects = nil
	for i := 0; i < 8; i++ {
		name := "short"
		if i == 7 {
			name = strings.Repeat("长", 25)
		}
		m.projects = append(m.projects, project{Name: name, Path: fmt.Sprint(i), Sessions: 1})
		m.matchingProjects = append(m.matchingProjects, i)
	}
	before := m.columns()[0].width
	m.projectCursor = 7
	m.clamp()
	after := m.columns()[0].width
	if after <= before || after < 57 {
		t.Fatalf("visible long row did not expand the column: %d -> %d", before, after)
	}
}

func TestBrowsingColumnsKeepContentWidthWhenWindowGrows(t *testing.T) {
	for _, level := range []navLevel{levelProject, levelSession, levelDirectory, levelFile, levelEdit} {
		t.Run(levelKey(level), func(t *testing.T) {
			m := demoModel(t)
			m.level, m.focusDiff = level, false
			m.width = 158
			before := m.columns()
			m.width = 220
			after := m.columns()
			if len(before) != len(after) {
				t.Fatalf("unexpected column count change: %v -> %v", before, after)
			}
			for i := 0; i < len(before)-1; i++ {
				if after[i].width != before[i].width {
					t.Fatalf("%s column stretched for unchanged content: %d -> %d", levelKey(after[i].level), before[i].width, after[i].width)
				}
			}
			if got := after[len(after)-1].width - before[len(before)-1].width; got != 62 {
				t.Fatalf("right preview did not receive the 62 extra cells: %d", got)
			}
		})
	}
}

func TestAdaptiveListsAndMenusStayInsideTerminal(t *testing.T) {
	for _, width := range []int{40, 64, 89, 90, 100, 158, 240} {
		for _, height := range []int{10, 24} {
			m := demoModel(t)
			m.width, m.height = width, height
			for level := levelProject; level <= levelHunk; level++ {
				m.level, m.focusDiff = level, false
				m.rebuildDiff()
				columns, total := m.columns(), 0
				for _, column := range columns {
					total += column.width
					if column.width < 1 {
						t.Fatalf("negative column at %dx%d: %+v", width, height, columns)
					}
					if !column.diff {
						for _, row := range m.navigationRows(column.level, column.width) {
							if ansi.StringWidth(row) != column.width {
								t.Fatalf("list row overflows before frame clipping: %q", ansi.Strip(row))
							}
						}
					}
				}
				if total+len(columns)-1 != width {
					t.Fatalf("columns do not fill %d: %+v", width, columns)
				}
				for _, prefix := range []rune{',', 'm'} {
					typeKey(m, prefix)
					rows := strings.Split(m.render(), "\n")
					if len(rows) != height || !strings.Contains(ansi.Strip(rows[height-2]), "Esc") {
						t.Fatalf("menu footer is inaccessible at %dx%d", width, height)
					}
					for _, row := range rows {
						if ansi.StringWidth(row) != width {
							t.Fatalf("menu row overflow: %q", ansi.Strip(row))
						}
					}
					m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
				}
			}
		}
	}
}

func TestMetadataReflowsDiffAndKeepsReadingPosition(t *testing.T) {
	m := demoModel(t)
	m.height = 12
	m.continuous, m.level, m.focusDiff = true, levelFile, false
	m.rebuildDiff()
	typeKey(m, 'J')
	before, offset := m.diffWidth(), m.diffOffset
	chooseListOption(m, 'm', 'n')
	if m.diffWidth() <= before || m.diffOffset != offset {
		t.Fatalf("metadata change did not reflow or reset reading position: width %d -> %d, offset %d -> %d", before, m.diffWidth(), offset, m.diffOffset)
	}
	for _, row := range m.rows {
		if ansi.StringWidth(row.text) > m.diffWidth() {
			t.Fatal("diff retained its old width after the list changed")
		}
	}
}
