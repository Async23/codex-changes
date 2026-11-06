package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// applyWorkingDirectory chooses the initial browsing context without opening a
// stored diff. Explicit --thread startup bypasses this function entirely.
func (m *model) applyWorkingDirectory() {
	m.filtering, m.focusDiff, m.continuous, m.hideFiles = false, false, false, false
	m.level = levelProject
	if m.opts.WorkingDirectory == "" {
		m.filterThreads()
		m.status = "选择项目和会话，逐层查看修改"
		return
	}
	index := m.bestProjectForWorkingDirectory(m.opts.WorkingDirectory)
	if index < 0 {
		m.filterThreads()
		m.status = "当前目录没有关联会话 · 显示全部项目"
		return
	}
	m.projectQuery = ""
	m.filterProjects("")
	for cursor, projectIndex := range m.matchingProjects {
		if projectIndex == index {
			m.projectCursor = cursor
			break
		}
	}
	m.pickerQuery = ""
	m.filterThreads()
	m.level = levelSession
	// Reuse the last selection only when it belongs to this directory's project.
	for cursor, threadIndex := range m.matchingThreads {
		if m.threads[threadIndex].ID == m.opts.Preferences.LastThread {
			m.pickerCursor = cursor
			break
		}
	}
	m.clamp()
	if p := m.selectedProject(); p != nil {
		m.status = fmt.Sprintf("当前目录 · %s · %d 个会话", clean(p.Path), len(m.matchingThreads))
	}
}

func (m *model) bestProjectForWorkingDirectory(cwd string) int {
	cwd = filepath.Clean(cwd)
	home, _ := os.UserHomeDir()
	if home != "" {
		home = filepath.Clean(home)
	}
	root := string(filepath.Separator)
	best, bestLength := -1, -1
	for i, p := range m.projects {
		path := filepath.Clean(p.Path)
		rel, err := filepath.Rel(path, cwd)
		if err != nil || rel != "." && (rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			continue
		}
		// Home and filesystem root are catch-all locations, not useful parents
		// for every nested directory. Exact matches still select them.
		if rel != "." && (path == home || path == root) {
			continue
		}
		if len(path) > bestLength {
			best, bestLength = i, len(path)
		}
	}
	return best
}
