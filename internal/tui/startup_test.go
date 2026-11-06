package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Async23/codex-changes/internal/codex"
	"github.com/Async23/codex-changes/internal/preferences"
)

func startupThreads(t *testing.T) []codex.Thread {
	t.Helper()
	threads, err := codex.NewDemo().ListThreads(context.Background(), codex.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return threads
}

func startupModel(t *testing.T, cwd, lastThread string, threads []codex.Thread) *model {
	t.Helper()
	opts := Options{Demo: true, Children: true, WorkingDirectory: cwd, Preferences: preferences.Defaults()}
	opts.Preferences.LastThread = lastThread
	m := newModel(context.Background(), opts)
	m.reader = nil // Context selection is tested separately from automatic loading.
	_, cmd := m.Update(threadsMsg{threads: threads})
	if cmd != nil {
		t.Fatal("unexpected background load without a reader")
	}
	return m
}

func TestStartupUsesCurrentProjectWithoutOpeningStoredDiff(t *testing.T) {
	m := startupModel(t, "/demo/camera", "demo-camera", startupThreads(t))
	if m.level != levelSession || m.focusDiff || m.session.Thread.ID != "" {
		t.Fatalf("startup restored a deep diff: level=%d focus=%v session=%q", m.level, m.focusDiff, m.session.Thread.ID)
	}
	if p := m.selectedProject(); p == nil || p.Path != "/demo/camera" {
		t.Fatalf("wrong startup project: %+v", p)
	}
	if !strings.Contains(m.status, "当前目录") {
		t.Fatalf("startup context is not visible: %q", m.status)
	}
}

func TestStartupFindsNearestProjectFromSubdirectory(t *testing.T) {
	threads := append(startupThreads(t), codex.Thread{ID: "nested", Name: "嵌套项目", CWD: "/demo/camera/notes"})
	m := startupModel(t, "/demo/camera/notes/subdirectory", "", threads)
	if p := m.selectedProject(); p == nil || p.Path != "/demo/camera/notes" {
		t.Fatalf("did not choose nearest project: %+v", p)
	}
	if m.level != levelSession || m.selectedThreadID() != "nested" {
		t.Fatal("nearest project sessions were not selected")
	}
}

func TestStartupWithoutAssociationShowsProjects(t *testing.T) {
	m := startupModel(t, "/outside/any/history", "demo-camera", startupThreads(t))
	if m.level != levelProject || len(m.matchingProjects) != 3 {
		t.Fatalf("unrelated startup did not show global projects: level=%d projects=%d", m.level, len(m.matchingProjects))
	}
	if !strings.Contains(m.status, "没有关联会话") {
		t.Fatalf("fallback reason is not visible: %q", m.status)
	}
}

func TestStartupDoesNotTreatHomeAsUniversalParent(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	threads := append(startupThreads(t), codex.Thread{ID: "home", Name: "主目录杂项", CWD: home})
	m := startupModel(t, filepath.Join(home, "unrelated", "child"), "", threads)
	if m.level != levelProject {
		t.Fatal("unrelated home subdirectory was assigned to the catch-all home project")
	}
	m = startupModel(t, home, "", threads)
	if m.level != levelSession || m.selectedThreadID() != "home" {
		t.Fatal("an exact home startup did not select home sessions")
	}
}

func TestExplicitThreadOverridesWorkingDirectory(t *testing.T) {
	m := newModel(context.Background(), Options{Thread: "demo-camera", WorkingDirectory: "/demo/website", Preferences: preferences.Defaults()})
	_, _ = m.Update(readyMsg{reader: codex.NewDemo()})
	if m.cancelScan != nil {
		t.Cleanup(m.cancelScan)
	}
	if m.level != levelHunk || !m.loading || m.opts.Preferences.LastThread != "demo-camera" {
		t.Fatalf("explicit thread did not open directly: level=%d loading=%v", m.level, m.loading)
	}
}

func TestLastThreadAloneDoesNotOpenDiff(t *testing.T) {
	opts := Options{WorkingDirectory: "/outside", Preferences: preferences.Defaults()}
	opts.Preferences.LastThread = "demo-camera"
	m := newModel(context.Background(), opts)
	_, _ = m.Update(readyMsg{reader: codex.NewDemo()})
	if m.loading || m.level != levelProject || m.cancelScan != nil {
		t.Fatal("stored LastThread triggered a deep startup")
	}
}
