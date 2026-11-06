package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Async23/codex-changes/internal/changes"
	"github.com/Async23/codex-changes/internal/codex"
	"github.com/Async23/codex-changes/internal/preferences"
	"github.com/charmbracelet/x/ansi"
)

func chooseListOption(m *model, prefix, key rune) {
	typeKey(m, prefix)
	typeKey(m, key)
}

func listIDs(m *model, level navLevel) []string {
	var ids []string
	for _, index := range m.navigationIndices(level) {
		ids = append(ids, m.listEntry(level, index).id)
	}
	return ids
}

func TestSessionSortPreservesSelectionAndSurvivesFiltering(t *testing.T) {
	m := demoModel(t)
	m.reader = nil
	m.threads = []codex.Thread{
		{ID: "ten", Name: "会话10", CWD: "/demo/camera", UpdatedAt: 300, CreatedAt: 10},
		{ID: "two", Name: "会话2", CWD: "/demo/camera", UpdatedAt: 100, CreatedAt: 20},
		{ID: "unknown", Name: "会话1", CWD: "/demo/camera"},
	}
	m.matchingThreads = nil
	m.rebuildProjects()
	m.filterThreads()
	m.level, m.focusDiff = levelSession, false
	generation := m.generation
	for _, test := range []struct {
		key  rune
		want []string
	}{
		{'a', []string{"unknown", "ten", "two"}},
		{'n', []string{"unknown", "two", "ten"}},
		{'N', []string{"ten", "two", "unknown"}},
		{'m', []string{"two", "ten", "unknown"}},
		{'M', []string{"ten", "two", "unknown"}},
		{'b', []string{"ten", "two", "unknown"}},
		{'B', []string{"two", "ten", "unknown"}},
		{'d', []string{"ten", "two", "unknown"}},
	} {
		chooseListOption(m, ',', test.key)
		if got := listIDs(m, levelSession); !reflect.DeepEqual(got, test.want) {
			t.Fatalf("sort %c: got %v, want %v", test.key, got, test.want)
		}
		if m.selectedThreadID() != "ten" || m.generation != generation {
			t.Fatalf("sort %c changed the selected session or restarted loading", test.key)
		}
	}
	chooseListOption(m, ',', 'n')
	m.pickerQuery = "会话"
	m.filterThreads()
	if got := listIDs(m, levelSession); !reflect.DeepEqual(got, []string{"unknown", "two", "ten"}) {
		t.Fatalf("filter discarded natural sorting: %v", got)
	}
	if m.threads[0].ID != "ten" || m.threads[1].ID != "two" {
		t.Fatal("sorting mutated the underlying history")
	}
}

func TestSortKeepsChildrenAndEachLevelHasIndependentPreferences(t *testing.T) {
	m := demoModel(t)
	project, thread, directory, file, edit := m.selectedProject().Path, m.selectedThreadID(), m.selectedDirectory().Path, m.selectedPath(), m.selectedEdit().ID
	for _, level := range []navLevel{levelProject, levelDirectory, levelFile} {
		m.level, m.focusDiff = level, false
		chooseListOption(m, ',', 'C')
		if m.selectedProject().Path != project || m.selectedThreadID() != thread || m.selectedDirectory().Path != directory || m.selectedPath() != file || m.selectedEdit().ID != edit {
			t.Fatalf("sorting level %d changed its selected descendants", level)
		}
	}
	m.level = levelFile
	chooseListOption(m, ',', 'e')
	chooseListOption(m, 'm', 'n')
	if m.listPreference(levelFile).Sort != "extension" || m.listPreference(levelFile).Linemode != "none" {
		t.Fatal("file choices were not applied")
	}
	if m.listPreference(levelProject).Sort != "count" || m.listPreference(levelDirectory).Linemode != "count" || m.listPreference(levelSession).Sort != "" {
		t.Fatal("a list choice leaked into another navigation level")
	}

	path := t.TempDir() + "/preferences.json"
	if err := preferences.Save(path, m.opts.Preferences); err != nil {
		t.Fatal(err)
	}
	saved, err := preferences.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	next := newModel(context.Background(), Options{Preferences: saved})
	if next.listPreference(levelFile) != m.listPreference(levelFile) || next.listPreference(levelProject) != m.listPreference(levelProject) {
		t.Fatal("per-level preferences did not survive a restart")
	}
}

func TestFileSortingUsesHistoryAndSurvivesScanProgress(t *testing.T) {
	m := demoModel(t)
	m.session.Files = []changes.File{
		{Path: "/demo/camera/x/a10.go", Added: 10, Edits: []changes.Edit{{ID: "a", Time: 300}, {ID: "b", Time: 100}}},
		{Path: "/demo/camera/x/a2.md", Added: 2, Removed: 3, Edits: []changes.Edit{{ID: "c", Time: 200}}},
		{Path: "/demo/camera/x/a1.txt", Edits: []changes.Edit{{ID: "d"}}},
	}
	m.directories, m.matchingDirectories, m.files = nil, nil, nil
	m.rebuildDirectories("/demo/camera/x/a10.go")
	m.level, m.focusDiff = levelFile, false
	m.rebuildDiff()
	for _, test := range []struct {
		key  rune
		want []int
	}{
		{'n', []int{2, 1, 0}}, {'e', []int{0, 1, 2}},
		{'s', []int{2, 1, 0}}, {'C', []int{0, 1, 2}},
		{'m', []int{1, 0, 2}}, {'b', []int{0, 1, 2}},
	} {
		chooseListOption(m, ',', test.key)
		if !reflect.DeepEqual(m.files, test.want) || m.selectedPath() != "/demo/camera/x/a10.go" {
			t.Fatalf("file sort %c: order=%v selection=%q", test.key, m.files, m.selectedPath())
		}
	}
	d := m.selectedDirectory()
	if d.CreatedAt != 100 || d.UpdatedAt != 300 || d.Added != 12 || d.Removed != 3 || d.Files != 3 {
		t.Fatalf("directory metadata did not aggregate recorded history: %+v", d)
	}
	chooseListOption(m, ',', 'n')
	session := m.session
	session.Files = append(append([]changes.File(nil), session.Files...), changes.File{Path: "/demo/camera/x/a3.go"})
	m.Update(scanMsg{generation: m.generation, progress: changes.Progress{Session: session, Done: true}})
	if !reflect.DeepEqual(m.files, []int{2, 1, 3, 0}) || m.selectedPath() != "/demo/camera/x/a10.go" || m.selectedEdit().ID != "a" {
		t.Fatalf("scan progress lost order or selection: %v %s", m.files, m.selectedPath())
	}
}

func TestEditAndHunkSortPreserveDiffIdentity(t *testing.T) {
	m := demoModel(t)
	m.level, m.focusDiff = levelEdit, false
	id := m.selectedEdit().ID
	chooseListOption(m, ',', 'm')
	if m.selectedEdit().ID != id || m.editCursor != 1 {
		t.Fatal("sorting edits changed the selected edit")
	}
	m.toggleContinuous()
	if m.diffOffset != m.anchors[m.matchingEdits[m.editCursor]] {
		t.Fatal("continuous mode used a sorted cursor as an original edit index")
	}
	m.toggleContinuous()
	lines, added, removed := changes.ParseDiff("update", "@@ -1 +1 @@\n-old-one\n+new-one\n@@ -1 +1 @@\n-old-two\n+new-two\n+extra\n")
	m.selectedFile().Edits = []changes.Edit{{ID: "duplicate-titles", Lines: lines, Added: added, Removed: removed}}
	m.matchingEdits, m.matchingHunks, m.hunks, m.hunksEditID = nil, nil, nil, ""
	m.level, m.focusDiff = levelHunk, false
	m.rebuildDiff()
	m.move(1)
	chooseListOption(m, ',', 'S')
	if m.hunkCursor != 0 || !strings.Contains(ansi.Strip(m.render()), "new-two") || strings.Contains(ansi.Strip(m.render()), "new-one") {
		t.Fatal("sorting hunks with identical headers switched the preview")
	}
	chooseListOption(m, ',', 'd')
	if m.hunkCursor != 1 || !strings.Contains(ansi.Strip(m.render()), "new-two") {
		t.Fatal("restoring hunk order changed the selected patch")
	}
}

func TestListMenusDoNotLeakKeysOrMouseActions(t *testing.T) {
	m := demoModel(t)
	for _, key := range []rune{',', 'm'} {
		typeKey(m, key)
		if m.listMenu != "" {
			t.Fatal("diff opened a menu for a hidden list")
		}
	}
	m.level, m.focusDiff = levelFile, false
	typeKey(m, ',')
	before := m.selectedPath()
	typeKey(m, 'l')
	m.click(tea.Mouse{X: 0, Y: 0, Button: tea.MouseLeft})
	m.wheel(tea.Mouse{X: 0, Y: 5, Button: tea.MouseWheelDown})
	if m.level != levelFile || m.listMenu != "," || m.selectedPath() != before {
		t.Fatal("unrecognized menu input changed underlying navigation")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.listMenu != "" || m.level != levelFile {
		t.Fatal("Esc navigated out of the list instead of closing the menu")
	}
	typeKey(m, '/')
	for _, key := range ",mn" {
		typeKey(m, key)
	}
	if m.fileQuery != ",mn" || m.listMenu != "" {
		t.Fatal("list shortcuts intercepted filter text")
	}
}

func TestLineInformationModesAndUnknownTimes(t *testing.T) {
	m := demoModel(t)
	m.level, m.focusDiff = levelFile, false
	path := m.selectedPath()
	chooseListOption(m, 'm', 'n')
	name, info := m.navigationParts(levelFile, m.fileCursor)
	if info != "" || !strings.Contains(name, "读出时间") {
		t.Fatal("hidden metadata removed the name or left row information")
	}
	chooseListOption(m, 'm', 's')
	_, info = m.navigationParts(levelFile, m.fileCursor)
	if !strings.Contains(ansi.Strip(info), "+") || !strings.Contains(ansi.Strip(info), "−") || strings.Contains(info, "次") {
		t.Fatal("change information did not replace the default information")
	}
	chooseListOption(m, 'm', 'a')
	_, info = m.navigationParts(levelFile, m.fileCursor)
	if !strings.Contains(ansi.Strip(info), "2 次") || m.selectedPath() != path {
		t.Fatal("restoring default information changed the selection")
	}
	for i := range m.selectedFile().Edits {
		m.selectedFile().Edits[i].Time = 0
	}
	chooseListOption(m, 'm', 'm')
	_, info = m.navigationParts(levelFile, m.fileCursor)
	if ansi.Strip(info) != "时间未记录" {
		t.Fatalf("missing historical timestamp became a real date: %s", info)
	}
}

func TestNaturalOrderingHandlesLargeNumbersAndLeadingZeros(t *testing.T) {
	for _, pair := range [][2]string{
		{"文件2", "文件10"}, {"a09", "a10"}, {"a2b3", "a2b11"},
		{"a999999999999999999999999999999", "a1000000000000000000000000000000"},
		{"a02", "a2"}, {"a", "a0"},
	} {
		if naturalCompare(pair[0], pair[1]) >= 0 || naturalCompare(pair[1], pair[0]) <= 0 || naturalCompare(pair[0], pair[0]) != 0 {
			t.Fatalf("invalid natural order for %q and %q", pair[0], pair[1])
		}
	}
}

func TestUnsupportedSavedListChoicesFallBackToDefaults(t *testing.T) {
	m := demoModel(t)
	m.opts.Preferences.Lists = map[string]preferences.List{"session": {Sort: "extension", Reverse: true, Linemode: "changes"}}
	if got := m.listPreference(levelSession); got != (preferences.List{}) {
		t.Fatalf("unsupported options were applied to sessions: %+v", got)
	}
}
