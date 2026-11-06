package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Async23/codex-changes/internal/changes"
	"github.com/Async23/codex-changes/internal/codex"
	"github.com/Async23/codex-changes/internal/preferences"
)

type Options struct {
	Binary           string
	Thread           string
	File             string
	Demo             bool
	Children         bool
	Archived         bool
	WorkingDirectory string
	Preferences      preferences.State
	StatePath        string
}

type model struct {
	ctx                              context.Context
	opts                             Options
	reader                           changes.Reader
	client                           *codex.Client
	width, height                    int
	dark                             bool
	level                            navLevel
	startupContextApplied            bool
	projects                         []project
	matchingProjects                 []int
	projectQuery                     string
	projectCursor, projectOffset     int
	threads                          []codex.Thread
	matchingThreads                  []int
	pickerQuery                      string
	pickerCursor, pickerOffset       int
	session                          changes.Session
	directories                      []directory
	matchingDirectories              []int
	directoryQuery                   string
	directoryCursor, directoryOffset int
	files                            []int
	fileQuery                        string
	filtering                        bool
	listMenu                         string
	fileCursor, fileOffset           int
	matchingEdits                    []int
	editQuery                        string
	editCursor, editOffset           int
	hunks                            []editHunk
	matchingHunks                    []int
	hunkQuery                        string
	hunksEditID                      string
	hunkCursor, hunkOffset           int
	focusDiff                        bool
	continuous                       bool
	hideFiles                        bool
	diffOffset, diffColumn           int
	rows                             []displayRow
	anchors                          []int
	collapsed                        map[string]bool
	highlights                       highlightCache
	help                             bool
	loading                          bool
	listLoading                      bool
	status                           string
	err                              string
	generation                       int
	cancelScan                       context.CancelFunc
	updates                          <-chan changes.Progress
	spinner                          int
}

type readyMsg struct {
	reader changes.Reader
	client *codex.Client
	err    error
}
type threadsMsg struct {
	threads []codex.Thread
	err     error
}
type scanMsg struct {
	generation int
	progress   changes.Progress
}
type pulseMsg struct{}

func Run(ctx context.Context, opts Options) error {
	if opts.WorkingDirectory == "" {
		opts.WorkingDirectory, _ = os.Getwd()
	}
	m := newModel(ctx, opts)
	final, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if m, ok := final.(*model); ok {
		if m.cancelScan != nil {
			m.cancelScan()
		}
		if m.client != nil {
			_ = m.client.Close()
		}
		if !opts.Demo {
			if saveErr := preferences.Save(opts.StatePath, m.opts.Preferences); err == nil {
				err = saveErr
			}
		}
	}
	return err
}

func newModel(ctx context.Context, opts Options) *model {
	if opts.Preferences.Theme == "" {
		opts.Preferences = preferences.Defaults()
	}
	return &model{ctx: ctx, opts: opts, dark: true, level: levelProject, listLoading: true, status: "正在连接 Codex…", collapsed: make(map[string]bool)}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(func() tea.Msg {
		if m.opts.Demo {
			return readyMsg{reader: codex.NewDemo()}
		}
		client, err := codex.Start(m.ctx, m.opts.Binary)
		return readyMsg{reader: client, client: client, err: err}
	}, tea.RequestBackgroundColor, pulse())
}

func pulse() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return pulseMsg{} })
}

func (m *model) listCmd() tea.Cmd {
	reader, ctx, archived := m.reader, m.ctx, m.opts.Archived
	return func() tea.Msg {
		threads, err := reader.ListThreads(ctx, codex.ListOptions{})
		if err == nil && archived {
			var old []codex.Thread
			old, err = reader.ListThreads(ctx, codex.ListOptions{Archived: true})
			threads = append(threads, old...)
		}
		return threadsMsg{threads: threads, err: err}
	}
}

func awaitScan(generation int, updates <-chan changes.Progress) tea.Cmd {
	return func() tea.Msg {
		progress, ok := <-updates
		if !ok {
			return nil
		}
		return scanMsg{generation: generation, progress: progress}
	}
}

func (m *model) openThread(id string, target navLevel) tea.Cmd {
	if m.reader == nil || id == "" {
		return nil
	}
	previousThread := m.opts.Preferences.LastThread
	if m.cancelScan != nil {
		m.cancelScan()
	}
	m.selectProjectForThread(id)
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelScan = cancel
	m.generation++
	m.loading, m.err, m.status = true, "", "正在读取修改记录…"
	m.level, m.focusDiff = target, target == levelHunk
	m.hideFiles = false
	m.fileQuery = ""
	m.directoryQuery, m.editQuery, m.hunkQuery = "", "", ""
	if target != levelSession {
		m.filtering = false
	}
	m.fileCursor, m.fileOffset = 0, 0
	m.editCursor, m.editOffset = 0, 0
	m.hunkCursor, m.hunkOffset = 0, 0
	m.diffOffset, m.diffColumn = 0, 0
	m.session, m.directories, m.files, m.rows = changes.Session{}, nil, nil, nil
	m.matchingDirectories, m.matchingEdits = nil, nil
	m.hunks, m.matchingHunks, m.hunksEditID = nil, nil, ""
	m.collapsed = make(map[string]bool)
	m.highlights = highlightCache{}
	if previousThread != id {
		m.opts.Preferences.LastFile = ""
	}
	m.opts.Preferences.LastThread = id
	updates := make(chan changes.Progress, 1)
	m.updates = updates
	reader, children := m.reader, m.opts.Children
	go func() {
		defer close(updates)
		changes.Scan(ctx, reader, id, children, func(progress changes.Progress) {
			select {
			case updates <- progress:
			case <-ctx.Done():
			}
		})
	}()
	return awaitScan(m.generation, updates)
}

func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.rebuildDiff()
		m.clamp()
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		m.rebuildDiff()
	case readyMsg:
		if msg.err != nil {
			m.listLoading = false
			m.err = msg.err.Error()
			m.status = "连接失败 · r 重试"
			return m, nil
		}
		m.reader, m.client = msg.reader, msg.client
		if m.opts.Thread != "" {
			return m, tea.Batch(m.listCmd(), m.openThread(m.opts.Thread, levelHunk))
		}
		return m, m.listCmd()
	case threadsMsg:
		m.listLoading = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.threads = msg.threads
		m.rebuildProjects()
		if !m.startupContextApplied && m.opts.Thread == "" {
			m.startupContextApplied = true
			m.applyWorkingDirectory()
			return m, m.previewSelectedThread()
		}
		m.startupContextApplied = true
		id := m.session.Thread.ID
		if id == "" {
			id = m.opts.Preferences.LastThread
		}
		m.selectProjectForThread(id)
		m.filterThreads()
		if m.session.Thread.ID == "" && !m.loading {
			m.status = "选择项目和会话，逐层查看修改"
		}
		return m, m.previewSelectedThread()
	case scanMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		previousPath := m.selectedPath()
		previousEdit := ""
		if edit := m.selectedEdit(); edit != nil {
			previousEdit = edit.ID
		}
		m.session = msg.progress.Session
		preferred := previousPath
		if m.opts.File != "" {
			preferred = m.opts.File
		} else if previousPath == "" {
			preferred = m.opts.Preferences.LastFile
		}
		m.rebuildDirectories(preferred)
		m.selectFile(preferred)
		if previousPath != "" && previousPath == m.selectedPath() {
			m.filterEdits()
			if file := m.selectedFile(); file != nil {
				for i, index := range m.matchingEdits {
					if file.Edits[index].ID == previousEdit {
						m.editCursor = i
						break
					}
				}
			}
		}
		m.rebuildDiff()
		m.loading = !msg.progress.Done
		m.status = fmt.Sprintf("已读 %d 轮 · %d 个文件 · %d 次修改", m.session.TurnsRead, len(m.session.Files), m.session.EditCount())
		if msg.progress.Err != nil {
			m.err = msg.progress.Err.Error()
			m.status = "读取未完成 · r 重试"
		}
		if msg.progress.Done {
			if m.opts.File != "" && msg.progress.Err == nil && !m.selectFile(m.opts.File) {
				m.err = fmt.Sprintf("这份历史中找不到文件 %q；可返回文件层重新筛选", m.opts.File)
			}
			m.opts.File = ""
			m.opts.Preferences.LastFile = m.selectedPath()
			m.rebuildDiff()
			if len(m.session.Warnings) > 0 {
				m.status += fmt.Sprintf(" · %d 条读取提示（? 查看）", len(m.session.Warnings))
			}
			return m, nil
		}
		return m, awaitScan(m.generation, m.updates)
	case pulseMsg:
		m.spinner = (m.spinner + 1) % 4
		return m, pulse()
	case clipboardMsg:
		if msg.err != nil {
			m.status = "复制失败：" + msg.err.Error()
		} else if msg.native {
			m.status = "已复制完整路径"
		} else {
			m.status = "已向终端发送复制请求（OSC 52）"
			return m, tea.SetClipboard(msg.text)
		}
	case tea.KeyPressMsg:
		return m, m.key(msg)
	case tea.PasteMsg:
		if m.filtering {
			m.setCurrentQuery(m.currentQuery() + inputText(msg.Content))
			return m, m.applyCurrentFilter()
		}
	case tea.MouseClickMsg:
		return m, m.click(msg.Mouse())
	case tea.MouseWheelMsg:
		return m, m.wheel(msg.Mouse())
	}
	return m, nil
}

func (m *model) key(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if key == "ctrl+c" {
		return tea.Quit
	}
	if m.help {
		if key == "esc" || key == "?" {
			m.help = false
		}
		return nil
	}
	if m.filtering {
		return m.filterKey(msg)
	}
	if m.listMenu != "" {
		m.listMenuKey(key)
		return nil
	}
	if key == "q" {
		return tea.Quit
	}
	if key == "?" {
		m.help = true
		return nil
	}
	switch key {
	case ",", "m":
		if !m.focusDiff {
			m.listMenu = key
		}
	case "s":
		return m.showSessionPicker()
	case "ctrl+r":
		m.listLoading = true
		if m.reader == nil {
			return m.Init()
		}
		return m.listCmd()
	case "r":
		if m.level <= levelSession {
			m.listLoading = true
			if m.reader == nil {
				return m.Init()
			}
			return m.listCmd()
		}
		if m.reader == nil {
			m.listLoading, m.err = true, ""
			return m.Init()
		}
		return m.openThread(m.opts.Preferences.LastThread, m.level)
	case "tab", "shift+tab":
		m.cycleFocus(key == "shift+tab")
	case "/", "f":
		if !m.focusDiff {
			m.filtering = true
		}
	case "esc":
		if m.currentQuery() != "" {
			m.setCurrentQuery("")
			return m.applyCurrentFilter()
		}
		m.back()
	case "enter":
		return m.drillDown()
	case "b":
		if !m.diffVisible() && !m.hideFiles {
			break
		}
		m.hideFiles = !m.hideFiles
		if m.hideFiles {
			if !m.continuous {
				m.level = levelHunk
			}
			m.focusDiff = true
		} else {
			m.focusDiff = false
		}
		m.rebuildDiff()
	case "c":
		if m.level >= levelFile {
			m.toggleContinuous()
		}
	case "w":
		if !m.diffVisible() {
			break
		}
		m.opts.Preferences.Wrap = !m.opts.Preferences.Wrap
		m.diffColumn = 0
		m.rebuildDiff()
	case "v":
		if !m.diffVisible() {
			break
		}
		m.opts.Preferences.Split = !m.opts.Preferences.Split
		m.rebuildDiff()
	case "t":
		m.toggleTheme()
	case "y":
		if m.level >= levelFile {
			if path := m.selectedPath(); path != "" {
				return copyPath(path)
			}
		}
	case "up", "k":
		return m.move(-1)
	case "down", "j":
		return m.move(1)
	case "pgup", "ctrl+b":
		return m.move(-m.pageSize(true))
	case "pgdown", "ctrl+f":
		return m.move(m.pageSize(true))
	case "ctrl+u":
		return m.move(-m.pageSize(false))
	case "ctrl+d":
		return m.move(m.pageSize(false))
	case "home", "g":
		if m.focusDiff {
			m.diffOffset = 0
			m.clamp()
		} else {
			return m.move(-1 << 30)
		}
	case "end", "G":
		if m.focusDiff {
			m.diffOffset = len(m.rows)
			m.clamp()
		} else {
			return m.move(1 << 30)
		}
	case "J":
		if m.diffVisible() {
			m.diffOffset += 5
			m.clamp()
		}
	case "K":
		if m.diffVisible() {
			m.diffOffset -= 5
			m.clamp()
		}
	case "left":
		if m.focusDiff && !m.opts.Preferences.Wrap {
			m.diffColumn = max(0, m.diffColumn-8)
			m.rebuildDiff()
		} else {
			m.back()
		}
	case "h":
		m.back()
	case "right", "l":
		if m.focusDiff && !m.opts.Preferences.Wrap {
			m.diffColumn = min(m.maxColumn(), m.diffColumn+8)
			m.rebuildDiff()
		} else {
			return m.drillDown()
		}
	case "[":
		m.jumpEdit(-1)
	case "]":
		m.jumpEdit(1)
	case "space":
		m.toggleEdit()
	}
	return nil
}

func (m *model) currentQuery() string {
	return m.currentQueryFor(m.level)
}

func (m *model) currentQueryFor(level navLevel) string {
	switch level {
	case levelProject:
		return m.projectQuery
	case levelSession:
		return m.pickerQuery
	case levelDirectory:
		return m.directoryQuery
	case levelFile:
		return m.fileQuery
	case levelEdit:
		return m.editQuery
	case levelHunk:
		return m.hunkQuery
	}
	return ""
}

func (m *model) setCurrentQuery(query string) {
	switch m.level {
	case levelProject:
		m.projectQuery = query
	case levelSession:
		m.pickerQuery = query
	case levelDirectory:
		m.directoryQuery = query
	case levelFile:
		m.fileQuery = query
	case levelEdit:
		m.editQuery = query
	case levelHunk:
		m.hunkQuery = query
	}
}

func (m *model) filterKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		m.filtering = false
		return nil
	case "esc":
		m.filtering = false
		m.setCurrentQuery("")
	case "backspace":
		m.setCurrentQuery(backspace(m.currentQuery()))
	case "ctrl+u":
		m.setCurrentQuery("")
	default:
		m.setCurrentQuery(m.currentQuery() + inputText(msg.Key().Text))
	}
	return m.applyCurrentFilter()
}

func (m *model) applyCurrentFilter() tea.Cmd {
	switch m.level {
	case levelProject:
		m.filterProjects("")
		m.projectSelectionChanged()
	case levelSession:
		m.filterThreads()
		return m.previewSelectedThread()
	case levelDirectory:
		m.filterDirectories("")
		m.directorySelectionChanged()
	case levelFile:
		m.filterFiles("")
		m.rebuildDiff()
	case levelEdit:
		m.filterEdits()
		m.rebuildDiff()
	case levelHunk:
		m.filterHunks("")
		m.diffOffset = 0
		m.rebuildDiff()
	}
	return nil
}

func (m *model) diffVisible() bool {
	for _, column := range m.columns() {
		if column.diff {
			return true
		}
	}
	return false
}

func (m *model) previewSelectedThread() tea.Cmd {
	if m.level != levelSession || m.focusDiff || m.reader == nil {
		return nil
	}
	id := m.selectedThreadID()
	if id == "" || id == m.session.Thread.ID {
		return nil
	}
	if m.loading && m.opts.Preferences.LastThread == id {
		return nil
	}
	return m.openThread(id, levelSession)
}

func (m *model) pageSize(full bool) int {
	size := m.listPage()
	if m.focusDiff {
		size = m.diffHeight()
	}
	if !full {
		size /= 2
	}
	return max(1, size)
}

func (m *model) toggleTheme() {
	switch m.opts.Preferences.Theme {
	case "auto":
		m.opts.Preferences.Theme = "dark"
	case "dark":
		m.opts.Preferences.Theme = "light"
	default:
		m.opts.Preferences.Theme = "auto"
	}
	m.rebuildDiff()
}

func (m *model) drillDown() tea.Cmd {
	m.hideFiles = false
	if m.continuous {
		m.focusDiff = true
		m.rebuildDiff()
		return nil
	}
	switch m.level {
	case levelProject:
		if len(m.matchingThreads) > 0 {
			m.level = levelSession
			return m.previewSelectedThread()
		}
	case levelSession:
		if id := m.selectedThreadID(); id != "" {
			if id == m.session.Thread.ID || m.loading && m.opts.Preferences.LastThread == id {
				m.level = levelDirectory
				m.filtering = false
			} else {
				return m.openThread(id, levelDirectory)
			}
		}
	case levelDirectory:
		if len(m.files) > 0 {
			m.level = levelFile
		}
	case levelFile:
		if m.selectedFile() != nil && len(m.matchingEdits) > 0 {
			m.level = levelEdit
		}
	case levelEdit:
		if len(m.matchingHunks) > 0 {
			m.level = levelHunk
		}
	case levelHunk:
		if m.selectedHunk() != nil {
			m.focusDiff = true
		}
	}
	if m.level != levelHunk {
		m.focusDiff = false
	}
	m.rebuildDiff()
	m.clamp()
	return nil
}

func (m *model) back() {
	if m.hideFiles {
		m.hideFiles, m.focusDiff = false, false
	} else if m.focusDiff {
		m.focusDiff = false
	} else if m.continuous {
		m.continuous = false
		m.level = levelDirectory
	} else if m.level > levelProject {
		m.level--
	}
	if m.level <= levelSession {
		m.syncUpperSelection()
	}
	m.rebuildDiff()
	m.clamp()
}

func (m *model) cycleFocus(reverse bool) {
	columns := m.columns()
	if len(columns) == 0 {
		return
	}
	current := 0
	for i, column := range columns {
		if column.diff && m.focusDiff || !column.diff && !m.focusDiff && column.level == m.level {
			current = i
			break
		}
	}
	if reverse {
		current--
	} else {
		current++
	}
	current = (current + len(columns)) % len(columns)
	m.focusDiff = columns[current].diff
	if !m.focusDiff {
		m.level = columns[current].level
	}
	m.rebuildDiff()
	m.clamp()
}

func (m *model) toggleContinuous() {
	m.continuous = !m.continuous
	m.hideFiles = false
	m.diffOffset, m.diffColumn = 0, 0
	if m.continuous {
		m.level, m.focusDiff = levelFile, true
		m.editQuery, m.hunkQuery = "", ""
		m.filterEdits()
	} else {
		m.level, m.focusDiff = levelHunk, true
	}
	m.rebuildDiff()
	if m.continuous && m.editCursor >= 0 && m.editCursor < len(m.matchingEdits) {
		m.diffOffset = m.anchors[m.matchingEdits[m.editCursor]]
		m.clamp()
	}
}

func (m *model) move(delta int) tea.Cmd {
	if m.focusDiff {
		m.diffOffset += delta
		m.clamp()
		return nil
	}
	switch m.level {
	case levelProject:
		m.projectCursor += delta
		m.clamp()
		m.projectSelectionChanged()
	case levelSession:
		m.pickerCursor += delta
		m.clamp()
	case levelDirectory:
		m.directoryCursor += delta
		m.clamp()
		m.directorySelectionChanged()
	case levelFile:
		m.fileCursor += delta
		m.fileSelectionChanged()
	case levelEdit:
		m.editCursor += delta
		m.editSelectionChanged()
	case levelHunk:
		m.hunkCursor += delta
		m.hunkSelectionChanged()
	}
	return m.previewSelectedThread()
}

func (m *model) jumpEdit(direction int) {
	if !m.continuous {
		if m.level == levelEdit {
			m.editCursor += direction
			m.editSelectionChanged()
			return
		}
		if m.level != levelHunk {
			return
		}
		m.editCursor += direction
		m.level, m.focusDiff = levelHunk, true
		m.editSelectionChanged()
		return
	}
	m.focusDiff = true
	if direction > 0 {
		for _, offset := range m.anchors {
			if offset > m.diffOffset {
				m.diffOffset = offset
				m.clamp()
				return
			}
		}
	} else {
		for i := len(m.anchors) - 1; i >= 0; i-- {
			if m.anchors[i] < m.diffOffset {
				m.diffOffset = m.anchors[i]
				m.clamp()
				return
			}
		}
	}
}

func (m *model) toggleEdit() {
	if !m.continuous || len(m.rows) == 0 {
		return
	}
	row := m.rows[min(m.diffOffset, len(m.rows)-1)]
	m.collapsed[row.editID] = !m.collapsed[row.editID]
	m.rebuildDiff()
}

func backspace(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	return string(r[:len(r)-1])
}

func inputText(s string) string {
	return strings.NewReplacer("\n", " ", "\t", " ").Replace(changes.SafeText(s))
}

func visibleStart(start, cursor, size, length int) int {
	if cursor < start {
		start = cursor
	}
	if cursor >= start+size {
		start = cursor - size + 1
	}
	return max(0, min(start, max(0, length-size)))
}

func (m *model) clamp() {
	m.projectCursor = max(0, min(m.projectCursor, len(m.matchingProjects)-1))
	m.pickerCursor = max(0, min(m.pickerCursor, len(m.matchingThreads)-1))
	m.directoryCursor = max(0, min(m.directoryCursor, len(m.matchingDirectories)-1))
	m.fileCursor = max(0, min(m.fileCursor, len(m.files)-1))
	editCount, hunkCount := len(m.matchingEdits), len(m.matchingHunks)
	m.editCursor = max(0, min(m.editCursor, editCount-1))
	m.hunkCursor = max(0, min(m.hunkCursor, hunkCount-1))
	page := max(1, m.listPage())
	m.projectOffset = visibleStart(m.projectOffset, m.projectCursor, page, len(m.matchingProjects))
	m.pickerOffset = visibleStart(m.pickerOffset, m.pickerCursor, page, len(m.matchingThreads))
	m.directoryOffset = visibleStart(m.directoryOffset, m.directoryCursor, page, len(m.matchingDirectories))
	m.fileOffset = visibleStart(m.fileOffset, m.fileCursor, page, len(m.files))
	m.editOffset = visibleStart(m.editOffset, m.editCursor, page, editCount)
	m.hunkOffset = visibleStart(m.hunkOffset, m.hunkCursor, page, hunkCount)
	m.diffOffset = max(0, min(m.diffOffset, max(0, len(m.rows)-m.diffHeight())))
}

func (m *model) bodyHeight() int { return max(1, m.height-5) }
func (m *model) diffHeight() int { return max(1, m.bodyHeight()-2) }
func (m *model) listPage() int   { return max(1, m.bodyHeight()-2) }
func (m *model) narrow() bool    { return m.width < 90 }
func (m *model) sidebarWidth() int {
	for _, column := range m.columns() {
		if !column.diff && column.level == levelFile {
			return column.width
		}
	}
	return 0
}
func (m *model) diffWidth() int {
	for _, column := range m.columns() {
		if column.diff {
			return column.width
		}
	}
	return max(1, m.width)
}

func (m *model) showSessionPicker() tea.Cmd {
	m.continuous, m.hideFiles, m.focusDiff = false, false, false
	m.level = levelSession
	m.pickerQuery = ""
	m.syncUpperSelection()
	m.filterThreads()
	return m.previewSelectedThread()
}

func (m *model) click(mouse tea.Mouse) tea.Cmd {
	if mouse.Button != tea.MouseLeft || m.help || m.listMenu != "" {
		return nil
	}
	if mouse.Y <= 1 {
		return m.showSessionPicker()
	}
	column := m.columnAt(mouse.X)
	if column == nil || mouse.Y < 3 || mouse.Y >= m.height-2 {
		return nil
	}
	if column.diff {
		m.focusDiff = true
		index := m.diffOffset + mouse.Y - 5
		if m.continuous && mouse.Y >= 5 && index < len(m.rows) && m.rows[index].header {
			id := m.rows[index].editID
			m.collapsed[id] = !m.collapsed[id]
			m.rebuildDiff()
		}
		return nil
	}
	m.level, m.focusDiff = column.level, false
	if mouse.Y == 4 {
		m.filtering = true
		return nil
	}
	if mouse.Y < 5 {
		return nil
	}
	index := m.listOffset(m.level) + mouse.Y - 5
	if !m.setCursor(m.level, index) {
		return nil
	}
	return m.previewSelectedThread()
}

func (m *model) wheel(mouse tea.Mouse) tea.Cmd {
	if m.help || m.listMenu != "" {
		return nil
	}
	delta := 3
	if mouse.Button == tea.MouseWheelUp {
		delta = -3
	} else if mouse.Button != tea.MouseWheelDown {
		return nil
	}
	column := m.columnAt(mouse.X)
	if column == nil {
		return nil
	}
	if column.diff {
		m.focusDiff = true
	} else {
		m.level, m.focusDiff = column.level, false
	}
	return m.move(delta)
}

func (m *model) listOffset(level navLevel) int {
	switch level {
	case levelProject:
		return m.projectOffset
	case levelSession:
		return m.pickerOffset
	case levelDirectory:
		return m.directoryOffset
	case levelFile:
		return m.fileOffset
	case levelEdit:
		return m.editOffset
	case levelHunk:
		return m.hunkOffset
	}
	return 0
}

func (m *model) setCursor(level navLevel, index int) bool {
	m.level = level
	switch level {
	case levelProject:
		if index >= len(m.matchingProjects) {
			return false
		}
		m.projectCursor = index
		m.projectSelectionChanged()
	case levelSession:
		if index >= len(m.matchingThreads) {
			return false
		}
		m.pickerCursor = index
	case levelDirectory:
		if index >= len(m.matchingDirectories) {
			return false
		}
		m.directoryCursor = index
		m.directorySelectionChanged()
	case levelFile:
		if index >= len(m.files) {
			return false
		}
		m.fileCursor = index
		m.fileSelectionChanged()
	case levelEdit:
		if m.selectedFile() == nil || index >= len(m.matchingEdits) {
			return false
		}
		m.editCursor = index
		m.editSelectionChanged()
	case levelHunk:
		if index >= len(m.matchingHunks) {
			return false
		}
		m.hunkCursor = index
		m.hunkSelectionChanged()
	}
	m.clamp()
	return true
}
