package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Async23/codex-changes/internal/changes"
)

type navLevel uint8

const (
	levelProject navLevel = iota
	levelSession
	levelDirectory
	levelFile
	levelEdit
	levelHunk
)

type project struct {
	Path      string
	Name      string
	Sessions  int
	UpdatedAt int64
}

type directory struct {
	Path  string
	Label string
	Files int
}

type editHunk struct {
	Title          string
	Start, End     int
	Added, Removed int
}

func (m *model) selectedProject() *project {
	if m.projectCursor < 0 || m.projectCursor >= len(m.projects) {
		return nil
	}
	return &m.projects[m.projectCursor]
}

func (m *model) rebuildProjects() {
	selected := ""
	if p := m.selectedProject(); p != nil {
		selected = p.Path
	}
	if selected == "" && m.session.Thread.CWD != "" {
		selected = m.session.Thread.CWD
	}
	byPath := make(map[string]*project)
	for _, thread := range m.threads {
		path := filepath.Clean(thread.CWD)
		p := byPath[path]
		if p == nil {
			name := filepath.Base(path)
			if name == "." || name == string(filepath.Separator) || name == "" {
				name = path
			}
			p = &project{Path: path, Name: name}
			byPath[path] = p
		}
		p.Sessions++
		p.UpdatedAt = max(p.UpdatedAt, thread.UpdatedAt)
	}
	m.projects = m.projects[:0]
	for _, p := range byPath {
		m.projects = append(m.projects, *p)
	}
	sort.Slice(m.projects, func(i, j int) bool {
		if m.projects[i].UpdatedAt != m.projects[j].UpdatedAt {
			return m.projects[i].UpdatedAt > m.projects[j].UpdatedAt
		}
		return m.projects[i].Path < m.projects[j].Path
	})
	for i, p := range m.projects {
		if p.Path == selected {
			m.projectCursor = i
			break
		}
	}
	m.clamp()
}

func (m *model) selectProjectForThread(id string) {
	for _, thread := range m.threads {
		if thread.ID != id {
			continue
		}
		for i, p := range m.projects {
			if p.Path == filepath.Clean(thread.CWD) {
				m.projectCursor = i
				m.filterThreads()
				return
			}
		}
	}
}

func (m *model) selectedThreadIndex() int {
	if m.pickerCursor < 0 || m.pickerCursor >= len(m.matchingThreads) {
		return -1
	}
	return m.matchingThreads[m.pickerCursor]
}

func (m *model) selectedThreadID() string {
	if i := m.selectedThreadIndex(); i >= 0 {
		return m.threads[i].ID
	}
	return ""
}

func (m *model) filterThreads() {
	selected := m.selectedThreadID()
	m.matchingThreads = nil
	query := strings.ToLower(strings.TrimSpace(m.pickerQuery))
	projectPath := ""
	if p := m.selectedProject(); p != nil {
		projectPath = p.Path
	}
	for i, thread := range m.threads {
		// An empty query browses one project; typing searches all projects.
		if query == "" && (projectPath == "" || filepath.Clean(thread.CWD) != projectPath) {
			continue
		}
		if strings.Contains(strings.ToLower(thread.Title()+" "+thread.CWD+" "+thread.ID), query) {
			m.matchingThreads = append(m.matchingThreads, i)
		}
	}
	for i, index := range m.matchingThreads {
		if m.threads[index].ID == selected {
			m.pickerCursor = i
			break
		}
	}
	m.clamp()
}

func (m *model) syncUpperSelection() {
	if m.session.Thread.ID == "" {
		return
	}
	m.selectProjectForThread(m.session.Thread.ID)
	for i, index := range m.matchingThreads {
		if m.threads[index].ID == m.session.Thread.ID {
			m.pickerCursor = i
			break
		}
	}
}

func (m *model) directoryForPath(path string) directory {
	parent := filepath.Clean(filepath.Dir(path))
	label := parent
	if rel, err := filepath.Rel(m.session.Thread.CWD, parent); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		label = rel
	}
	return directory{Path: parent, Label: label}
}

func (m *model) selectedDirectory() *directory {
	if m.directoryCursor < 0 || m.directoryCursor >= len(m.directories) {
		return nil
	}
	return &m.directories[m.directoryCursor]
}

func (m *model) rebuildDirectories(preferredFile string) {
	selected := ""
	if d := m.selectedDirectory(); d != nil {
		selected = d.Path
	}
	byPath := make(map[string]*directory)
	for _, file := range m.session.Files {
		d := m.directoryForPath(file.Path)
		if existing := byPath[d.Path]; existing != nil {
			existing.Files++
			continue
		}
		copy := d
		copy.Files = 1
		byPath[d.Path] = &copy
	}
	m.directories = m.directories[:0]
	for _, d := range byPath {
		m.directories = append(m.directories, *d)
	}
	sort.Slice(m.directories, func(i, j int) bool { return m.directories[i].Label < m.directories[j].Label })
	if preferredFile != "" {
		selected = m.directoryForPath(preferredFile).Path
	}
	for i, d := range m.directories {
		if d.Path == selected {
			m.directoryCursor = i
			break
		}
	}
	m.filterFiles(preferredFile)
}

func (m *model) filterFiles(preferred string) {
	previous := m.selectedPath()
	m.files = nil
	query := strings.ToLower(strings.TrimSpace(m.fileQuery))
	directoryPath := ""
	if d := m.selectedDirectory(); d != nil {
		directoryPath = d.Path
	}
	for i, file := range m.session.Files {
		if directoryPath != "" && filepath.Clean(filepath.Dir(file.Path)) != directoryPath {
			continue
		}
		if strings.Contains(strings.ToLower(file.Path+" "+strings.Join(file.Aliases, " ")), query) {
			m.files = append(m.files, i)
		}
	}
	m.fileCursor = 0
	m.selectFilteredFile(preferred)
	if previous != m.selectedPath() {
		m.editCursor, m.hunkCursor = 0, 0
		m.hunks, m.hunksEditID = nil, ""
	}
	m.clamp()
}

func (m *model) selectFilteredFile(path string) bool {
	if path == "" {
		return false
	}
	for i, index := range m.files {
		f := m.session.Files[index]
		if f.Path == path || filepath.Base(f.Path) == path {
			m.fileCursor = i
			return true
		}
	}
	return false
}

func (m *model) selectFile(path string) bool {
	if m.selectFilteredFile(path) {
		return true
	}
	if path == "" || m.fileQuery != "" {
		return false
	}
	for _, file := range m.session.Files {
		if file.Path != path && filepath.Base(file.Path) != path {
			continue
		}
		wanted := m.directoryForPath(file.Path).Path
		for i, d := range m.directories {
			if d.Path == wanted {
				m.directoryCursor = i
				m.filterFiles(path)
				return m.selectedPath() == file.Path
			}
		}
	}
	return false
}

func (m *model) selectedFile() *changes.File {
	if m.fileCursor < 0 || m.fileCursor >= len(m.files) {
		return nil
	}
	return &m.session.Files[m.files[m.fileCursor]]
}

func (m *model) selectedPath() string {
	if f := m.selectedFile(); f != nil {
		return f.Path
	}
	return ""
}

func (m *model) selectedEdit() *changes.Edit {
	file := m.selectedFile()
	if file == nil || m.editCursor < 0 || m.editCursor >= len(file.Edits) {
		return nil
	}
	return &file.Edits[m.editCursor]
}

func buildEditHunks(edit *changes.Edit) []editHunk {
	if edit == nil || len(edit.Lines) == 0 {
		return nil
	}
	var starts []int
	for i, line := range edit.Lines {
		if line.Kind == changes.Hunk {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 {
		title := "完整补丁"
		if edit.Kind == "add" {
			title = "完整新建"
		} else if edit.Kind == "delete" {
			title = "完整删除"
		}
		h := editHunk{Title: title, Start: 0, End: len(edit.Lines)}
		for _, line := range edit.Lines {
			if line.Kind == changes.Added {
				h.Added++
			} else if line.Kind == changes.Removed {
				h.Removed++
			}
		}
		return []editHunk{h}
	}
	hunks := make([]editHunk, 0, len(starts))
	for i, header := range starts {
		start := header
		if i == 0 {
			start = 0 // Keep patch metadata with the first hunk.
		}
		end := len(edit.Lines)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		h := editHunk{Title: edit.Lines[header].Text, Start: start, End: end}
		if h.Title == "" {
			h.Title = fmt.Sprintf("差异块 %d", i+1)
		}
		for _, line := range edit.Lines[start:end] {
			if line.Kind == changes.Added {
				h.Added++
			} else if line.Kind == changes.Removed {
				h.Removed++
			}
		}
		hunks = append(hunks, h)
	}
	return hunks
}

func (m *model) refreshHunks() {
	edit := m.selectedEdit()
	if edit == nil {
		m.hunks, m.hunksEditID, m.hunkCursor = nil, "", 0
		return
	}
	previous := ""
	if m.hunksEditID == edit.ID && m.hunkCursor >= 0 && m.hunkCursor < len(m.hunks) {
		previous = m.hunks[m.hunkCursor].Title
	}
	m.hunks = buildEditHunks(edit)
	m.hunksEditID = edit.ID
	m.hunkCursor = 0
	for i, h := range m.hunks {
		if h.Title == previous {
			m.hunkCursor = i
			break
		}
	}
}

func (m *model) selectedHunk() *editHunk {
	if m.hunkCursor < 0 || m.hunkCursor >= len(m.hunks) {
		return nil
	}
	return &m.hunks[m.hunkCursor]
}

func (m *model) projectSelectionChanged() {
	m.pickerCursor, m.pickerOffset = 0, 0
	m.pickerQuery = ""
	m.filterThreads()
}

func (m *model) directorySelectionChanged() {
	m.filterFiles("")
	m.diffOffset, m.diffColumn = 0, 0
	m.rebuildDiff()
}

func (m *model) fileSelectionChanged() {
	m.clamp()
	m.editCursor, m.editOffset = 0, 0
	m.hunkCursor, m.hunkOffset = 0, 0
	m.hunks, m.hunksEditID = nil, ""
	m.diffOffset, m.diffColumn = 0, 0
	m.opts.Preferences.LastFile = m.selectedPath()
	m.rebuildDiff()
}

func (m *model) editSelectionChanged() {
	m.clamp()
	m.hunkCursor, m.hunkOffset = 0, 0
	m.hunks, m.hunksEditID = nil, ""
	m.diffOffset, m.diffColumn = 0, 0
	m.rebuildDiff()
}

func (m *model) hunkSelectionChanged() {
	m.clamp()
	m.diffOffset, m.diffColumn = 0, 0
	m.rebuildDiff()
}
