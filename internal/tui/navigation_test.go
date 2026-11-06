package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Async23/codex-changes/internal/changes"
	"github.com/Async23/codex-changes/internal/codex"
	"github.com/charmbracelet/x/ansi"
)

func TestMillerColumnDrillDownAndContinuousMode(t *testing.T) {
	m := demoModel(t)
	if len(m.projects) != 3 {
		t.Fatalf("got %d projects, want 3", len(m.projects))
	}
	m.level, m.focusDiff = levelDirectory, false
	if m.selectedDirectory() == nil || m.selectedDirectory().Label != "notes" {
		t.Fatalf("preferred file did not select its directory: %+v", m.selectedDirectory())
	}
	for want := levelFile; want <= levelHunk; want++ {
		m.drillDown()
		if m.level != want {
			t.Fatalf("drill down reached level %d, want %d", m.level, want)
		}
	}
	m.drillDown()
	if !m.focusDiff {
		t.Fatal("entering a hunk did not focus its diff")
	}
	text := ansi.Strip(m.render())
	if !strings.Contains(text, "单块") || !strings.Contains(text, "第 2 轮") {
		t.Fatalf("single-hunk view is missing context: %s", text)
	}
	typeKey(m, 'c')
	text = ansi.Strip(m.render())
	if !m.continuous || !strings.Contains(text, "第 1 轮") || !strings.Contains(text, "第 2 轮") {
		t.Fatal("continuous mode does not show the full edit timeline")
	}
}

func TestHunkSelectionLimitsRenderedDiff(t *testing.T) {
	m := demoModel(t)
	file := m.selectedFile()
	lines, added, removed := changes.ParseDiff("update", "@@ -1,2 +1,2 @@\n-first-old\n+first-new\n context\n@@ -20,2 +20,2 @@\n-second-old\n+second-new\n context\n")
	file.Edits = []changes.Edit{{ID: "synthetic-edit", Kind: "update", Lines: lines, Added: added, Removed: removed}}
	m.editCursor, m.hunkCursor = 0, 0
	m.hunks, m.hunksEditID = nil, ""
	m.level, m.focusDiff = levelHunk, true
	m.rebuildDiff()
	if len(m.hunks) != 2 {
		t.Fatalf("got %d hunks, want 2", len(m.hunks))
	}
	text := ansi.Strip(m.render())
	if !strings.Contains(text, "first-new") || strings.Contains(text, "second-new") {
		t.Fatalf("first hunk rendered unrelated content: %s", text)
	}
	m.focusDiff = false
	m.move(1)
	text = ansi.Strip(m.render())
	if strings.Contains(text, "first-new") || !strings.Contains(text, "second-new") {
		t.Fatalf("second hunk rendered unrelated content: %s", text)
	}
}

func TestExplicitSessionSearchPreservesText(t *testing.T) {
	m := demoModel(t)
	m.level, m.focusDiff = levelSession, false
	m.reader = nil
	typeKey(m, '/')
	for _, r := range "网站 hjq" {
		typeKey(m, r)
	}
	if m.level != levelSession || m.pickerQuery != "网站 hjq" {
		t.Fatalf("navigation keys leaked into session search: %q", m.pickerQuery)
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.pickerQuery != "网站 hj" {
		t.Fatalf("backspace failed in session search: %q", m.pickerQuery)
	}
}

func TestBackReturnsThroughHierarchy(t *testing.T) {
	m := demoModel(t)
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.level != levelHunk || m.focusDiff {
		t.Fatal("escape did not leave diff reading")
	}
	for _, want := range []navLevel{levelEdit, levelFile, levelDirectory, levelSession, levelProject} {
		m.back()
		if m.level != want {
			t.Fatalf("back reached level %d, want %d", m.level, want)
		}
	}
}

func TestYaziKeysWorkAtUpperLevels(t *testing.T) {
	m := demoModel(t)
	m.reader = nil // Keep navigation synchronous; automatic loading has its own test below.
	m.threads = append(m.threads, codex.Thread{ID: "synthetic-second", Name: "第二个会话", CWD: "/demo/camera"})
	m.rebuildProjects()
	m.selectProjectForThread("demo-camera")
	m.level, m.focusDiff = levelSession, false
	m.filterThreads()
	typeKey(m, 'j')
	if m.pickerCursor != 1 {
		t.Fatal("j did not select the next session")
	}
	typeKey(m, 'k')
	if m.pickerCursor != 0 {
		t.Fatal("k did not select the previous session")
	}
	typeKey(m, 'h')
	if m.level != levelProject {
		t.Fatal("h did not return to projects")
	}
	typeKey(m, 'l')
	if m.level != levelSession {
		t.Fatal("l did not enter sessions")
	}
}

func TestSessionHoverAutomaticallyLoads(t *testing.T) {
	m := demoModel(t)
	m.level, m.focusDiff = levelSession, false
	m.pickerQuery = "网站"
	m.opts.Preferences.LastFile = "/demo/camera/notes/读出时间.md"
	m.filterThreads()
	cmd := m.previewSelectedThread()
	if cmd == nil {
		t.Fatal("hovering another session did not start loading")
	}
	if m.opts.Preferences.LastFile != "" {
		t.Fatal("session preview inherited the previous session's file")
	}
	generation := m.generation
	m.drillDown()
	if m.level != levelDirectory || m.generation != generation {
		t.Fatal("entering a loading session restarted its scan")
	}
	for step := 0; cmd != nil && step < 32; step++ {
		msg := cmd()
		if msg == nil {
			break
		}
		_, next := m.Update(msg)
		cmd = next
	}
	if m.loading || m.session.Thread.ID != "demo-website" || len(m.directories) != 1 {
		t.Fatalf("automatic preview did not finish: loading=%v session=%q directories=%d", m.loading, m.session.Thread.ID, len(m.directories))
	}
}

func TestPreviewSeekDoesNotChangeFocus(t *testing.T) {
	m := demoModel(t)
	m.height = 12
	m.level, m.focusDiff = levelHunk, false
	m.rebuildDiff()
	typeKey(m, 'J')
	if m.diffOffset == 0 || m.focusDiff {
		t.Fatal("J did not scroll the preview without changing focus")
	}
	typeKey(m, 'K')
	if m.diffOffset != 0 || m.focusDiff {
		t.Fatal("K did not scroll the preview back")
	}
}

func TestNavigationLayoutReducesRedundancy(t *testing.T) {
	m := demoModel(t)
	m.level, m.focusDiff = levelSession, false
	columns := m.columns()
	if len(columns) != 3 || columns[2].width <= columns[0].width || columns[2].width <= columns[1].width {
		t.Fatalf("right preview does not receive the remaining space: %+v", columns)
	}
	text := ansi.Strip(m.render())
	if strings.Contains(text, "Enter 进入下一层") {
		t.Fatal("navigation hints are still repeated in every column")
	}
	if count := strings.Count(text, "/demo/camera"); count != 1 {
		t.Fatalf("selected path is repeated %d times", count)
	}
}
