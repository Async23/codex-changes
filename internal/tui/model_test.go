package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Async23/codex-changes/internal/changes"
	"github.com/Async23/codex-changes/internal/codex"
	"github.com/Async23/codex-changes/internal/preferences"
	"github.com/charmbracelet/x/ansi"
)

func demoModel(t *testing.T) *model {
	t.Helper()
	m := newModel(context.Background(), Options{Demo: true, Children: true, Preferences: preferences.Defaults()})
	m.width, m.height, m.listLoading = 158, 38, false
	m.level, m.focusDiff = levelHunk, true
	m.reader = codex.NewDemo()
	m.threads, _ = m.reader.ListThreads(context.Background(), codex.ListOptions{})
	m.rebuildProjects()
	m.selectProjectForThread("demo-camera")
	m.filterThreads()
	changes.Scan(context.Background(), m.reader, "demo-camera", true, func(p changes.Progress) {
		if p.Done {
			if p.Err != nil {
				t.Fatal(p.Err)
			}
			m.session = p.Session
		}
	})
	m.rebuildDirectories("")
	m.selectFile("读出时间.md")
	m.rebuildDiff()
	return m
}

func typeKey(m *model, key rune) { m.key(tea.KeyPressMsg{Code: key, Text: string(key)}) }

func TestFindFileWithoutBrowsingTurns(t *testing.T) {
	m := demoModel(t)
	m.continuous, m.level, m.focusDiff = true, levelFile, false
	m.rebuildDiff()
	typeKey(m, '/')
	for _, r := range "读出" {
		typeKey(m, r)
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.filtering || len(m.files) != 1 || len(m.selectedFile().Edits) != 2 {
		t.Fatalf("filter failed: %+v", m.files)
	}
	text := ansi.Strip(m.render())
	if !strings.Contains(text, "第 1 轮") || !strings.Contains(text, "第 2 轮") {
		t.Fatal("file history does not show both turns")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if len(m.files) != 1 || len(m.directories) != 4 {
		t.Fatal("escape did not clear the current directory filter")
	}
}

func TestTypingSpacesAndUnicodeBackspace(t *testing.T) {
	m := demoModel(t)
	m.level, m.focusDiff = levelSession, false
	m.reader = nil
	typeKey(m, '/')
	for _, r := range "camera 中文" {
		typeKey(m, r)
	}
	if m.pickerQuery != "camera 中文" {
		t.Fatalf("typing lost spaces: %q", m.pickerQuery)
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.pickerQuery != "camera 中" {
		t.Fatal("backspace corrupted Unicode")
	}
}

func TestMouseTargetsAndCollapse(t *testing.T) {
	m := demoModel(t)
	m.continuous, m.level, m.focusDiff = true, levelFile, false
	m.rebuildDiff()
	m.click(tea.Mouse{X: 3, Y: 5, Button: tea.MouseLeft})
	if !strings.HasSuffix(m.selectedPath(), "读出时间.md") {
		t.Fatal("mouse selected wrong file")
	}
	before := len(m.rows)
	m.click(tea.Mouse{X: m.sidebarWidth() + 5, Y: 5, Button: tea.MouseLeft})
	if len(m.rows) >= before || !m.focusDiff {
		t.Fatal("click did not collapse revision")
	}
	m.height = 16
	m.collapsed = map[string]bool{}
	m.selectFile("读出时间.md")
	m.rebuildDiff()
	m.wheel(tea.Mouse{X: m.width - 2, Y: 10, Button: tea.MouseWheelDown})
	if m.diffOffset <= 0 {
		t.Fatal("wheel did not scroll diff")
	}
	m.wheel(tea.Mouse{X: 3, Y: 10, Button: tea.MouseWheelUp})
	if m.focusDiff || m.fileCursor != 0 {
		t.Fatal("wheel did not route to file list")
	}
}

func TestStaleScanCannotReplaceNewSelection(t *testing.T) {
	m := demoModel(t)
	m.generation = 2
	m.Update(scanMsg{generation: 1, progress: changes.Progress{Done: true, Session: changes.Session{Thread: codex.Thread{ID: "stale"}}}})
	if m.session.Thread.ID != "demo-camera" {
		t.Fatal("stale scan overwrote current thread")
	}
}

func TestMissingRequestedFileIsReported(t *testing.T) {
	m := demoModel(t)
	m.opts.File = "not-present.md"
	m.Update(scanMsg{generation: m.generation, progress: changes.Progress{Done: true, Session: m.session}})
	if !strings.Contains(m.err, "找不到文件") {
		t.Fatal("missing --file silently selected an unrelated file")
	}
}

func TestLayoutsStayInsideTerminal(t *testing.T) {
	for _, width := range []int{40, 64, 89, 90, 100, 158, 240} {
		for _, height := range []int{10, 20, 38} {
			for _, split := range []bool{false, true} {
				t.Run(fmt.Sprintf("%dx%d/split=%v", width, height, split), func(t *testing.T) {
					m := demoModel(t)
					m.width, m.height = width, height
					m.opts.Preferences.Split = split
					f := m.selectedFile()
					f.Path = "/synthetic/很长的目录/" + strings.Repeat("中文长文件名", 12) + ".md"
					f.Edits[0].Lines = append(f.Edits[0].Lines, changes.Line{Kind: changes.Added, New: 123456, Text: strings.Repeat("中文 abc \t", 40)})
					for _, screen := range []string{"files", "diff", "projects", "sessions", "help"} {
						m.help = screen == "help"
						m.continuous, m.hideFiles = false, false
						m.level, m.focusDiff = levelHunk, true
						switch screen {
						case "files":
							m.continuous, m.level, m.focusDiff = true, levelFile, false
						case "projects":
							m.level, m.focusDiff = levelProject, false
						case "sessions":
							m.level, m.focusDiff = levelSession, false
						case "help":
							m.level, m.focusDiff = levelHunk, true
						}
						m.rebuildDiff()
						for _, wrap := range []bool{true, false} {
							m.opts.Preferences.Wrap = wrap
							m.rebuildDiff()
							for i, row := range m.rows {
								if got := ansi.StringWidth(row.text); got > m.diffWidth() {
									t.Fatalf("diff row %d overflows before frame clipping: %d > %d", i, got, m.diffWidth())
								}
							}
							rows := strings.Split(m.render(), "\n")
							if len(rows) != height {
								t.Fatalf("%s: %d rows, want %d", screen, len(rows), height)
							}
							for i, row := range rows {
								if got := ansi.StringWidth(row); got != width {
									t.Fatalf("%s row %d width %d, want %d: %q", screen, i, got, width, ansi.Strip(row))
								}
							}
						}
					}
				})
			}
		}
	}
}

func TestThemeWrapAndSplitActuallyChangeRendering(t *testing.T) {
	m := demoModel(t)
	m.opts.Preferences.Theme = "dark"
	m.rebuildDiff()
	dark := m.render()
	typeKey(m, 't')
	light := m.render()
	if dark == light || m.opts.Preferences.Theme != "light" {
		t.Fatal("theme toggle has no effect")
	}
	typeKey(m, 'v')
	if !m.opts.Preferences.Split || !strings.Contains(ansi.Strip(m.render()), "并排") {
		t.Fatal("split toggle has no effect")
	}
	typeKey(m, 'w')
	if m.opts.Preferences.Wrap {
		t.Fatal("wrap toggle has no effect")
	}
}
