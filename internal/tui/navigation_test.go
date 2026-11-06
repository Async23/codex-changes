package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Async23/codex-changes/internal/changes"
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

func TestUpperLevelsPreserveTextSearch(t *testing.T) {
	m := demoModel(t)
	m.level, m.focusDiff = levelProject, false
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
