package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Async23/codex-changes/internal/changes"
	"github.com/charmbracelet/x/ansi"
)

func TestFilterTargetsFocusedLevel(t *testing.T) {
	tests := []struct {
		name  string
		level navLevel
		query string
		valid func(*model) bool
	}{
		{"project", levelProject, "camera", func(m *model) bool {
			return len(m.matchingProjects) == 1 && m.selectedProject().Name == "camera"
		}},
		{"session", levelSession, "网站", func(m *model) bool {
			return len(m.matchingThreads) == 1 && m.selectedThreadID() == "demo-website"
		}},
		{"directory", levelDirectory, "notes", func(m *model) bool {
			return len(m.matchingDirectories) == 1 && m.selectedDirectory().Label == "notes"
		}},
		{"file", levelFile, "读出", func(m *model) bool {
			return len(m.files) == 1 && strings.HasSuffix(m.selectedPath(), "读出时间.md")
		}},
		{"edit", levelEdit, "整理", func(m *model) bool {
			return len(m.matchingEdits) == 1 && strings.Contains(m.selectedEdit().Prompt, "整理")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := demoModel(t)
			m.reader = nil // Filtering must not require an automatic session preview.
			m.level, m.focusDiff = test.level, false
			m.rebuildDiff()
			typeKey(m, '/')
			for _, r := range test.query {
				typeKey(m, r)
			}
			if m.level != test.level || !m.filtering || m.currentQuery() != test.query {
				t.Fatalf("filter moved to level %d with query %q", m.level, m.currentQuery())
			}
			if !test.valid(m) {
				t.Fatalf("focused level was not filtered correctly: %+v", m)
			}
		})
	}
}

func TestHunkFilterStaysInContext(t *testing.T) {
	m := demoModel(t)
	lines, added, removed := changes.ParseDiff("update", "@@ -1,2 +1,2 @@\n-first-old\n+first-new\n context\n@@ -20,2 +20,2 @@\n-second-old\n+second-new\n context\n")
	m.selectedFile().Edits = []changes.Edit{{ID: "synthetic-edit", Kind: "update", Lines: lines, Added: added, Removed: removed}}
	m.matchingEdits, m.hunks, m.matchingHunks, m.hunksEditID = nil, nil, nil, ""
	m.level, m.focusDiff = levelHunk, false
	m.rebuildDiff()
	typeKey(m, '/')
	for _, r := range "20" {
		typeKey(m, r)
	}
	if m.level != levelHunk || len(m.matchingHunks) != 1 || !strings.Contains(m.selectedHunk().Title, "-20") {
		t.Fatal("hunk filter did not select the matching hunk")
	}
	text := ansi.Strip(m.render())
	if strings.Contains(text, "first-new") || !strings.Contains(text, "second-new") {
		t.Fatalf("filtered hunk preview shows unrelated content: %s", text)
	}
}

func TestUpperLevelActionsDoNotTargetHiddenContent(t *testing.T) {
	m := demoModel(t)
	m.level, m.focusDiff = levelProject, false
	editCursor, diffOffset := m.editCursor, m.diffOffset
	split, wrap := m.opts.Preferences.Split, m.opts.Preferences.Wrap
	for _, key := range []rune{']', 'J', 'b', 'c', 'v', 'w'} {
		typeKey(m, key)
	}
	if m.level != levelProject || m.editCursor != editCursor || m.diffOffset != diffOffset || m.hideFiles || m.continuous {
		t.Fatal("upper-level action changed hidden navigation or diff state")
	}
	if m.opts.Preferences.Split != split || m.opts.Preferences.Wrap != wrap {
		t.Fatal("upper-level action changed hidden diff preferences")
	}
	if cmd := m.key(tea.KeyPressMsg{Code: 'y', Text: "y"}); cmd != nil {
		t.Fatal("project level attempted to copy a hidden file")
	}
	generation := m.generation
	if cmd := m.key(tea.KeyPressMsg{Code: 'r', Text: "r"}); cmd == nil || m.generation != generation || !m.listLoading {
		t.Fatal("r at project level did not refresh the visible list")
	}
}

func TestDiffFocusDoesNotFilterHiddenList(t *testing.T) {
	m := demoModel(t)
	typeKey(m, '/')
	if m.filtering || m.level != levelHunk || !m.focusDiff {
		t.Fatal("diff focus opened a filter for a hidden navigation list")
	}
}

func TestNarrowHiddenPreviewIsNotModified(t *testing.T) {
	m := demoModel(t)
	m.width = 64
	m.level, m.focusDiff = levelHunk, false
	m.rebuildDiff()
	split, wrap := m.opts.Preferences.Split, m.opts.Preferences.Wrap
	for _, key := range []rune{'J', 'K', 'v', 'w', 'b'} {
		typeKey(m, key)
	}
	if m.diffOffset != 0 || m.hideFiles || m.opts.Preferences.Split != split || m.opts.Preferences.Wrap != wrap {
		t.Fatal("narrow navigation modified a hidden diff")
	}
}
