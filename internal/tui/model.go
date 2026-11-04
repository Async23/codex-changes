package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Async23/codex-changes/internal/changes"
	"github.com/Async23/codex-changes/internal/codex"
	"github.com/Async23/codex-changes/internal/preferences"
)

type Options struct {
	Binary      string
	Thread      string
	File        string
	Demo        bool
	Children    bool
	Archived    bool
	Preferences preferences.State
	StatePath   string
}

type model struct {
	ctx                        context.Context
	opts                       Options
	reader                     changes.Reader
	client                     *codex.Client
	width, height              int
	dark                       bool
	threads                    []codex.Thread
	matchingThreads            []int
	picker                     bool
	pickerQuery                string
	pickerCursor, pickerOffset int
	session                    changes.Session
	files                      []int
	fileQuery                  string
	filtering                  bool
	fileCursor, fileOffset     int
	focusDiff                  bool
	hideFiles                  bool
	diffOffset, diffColumn     int
	rows                       []displayRow
	anchors                    []int
	collapsed                  map[string]bool
	help                       bool
	loading                    bool
	listLoading                bool
	status                     string
	err                        string
	generation                 int
	cancelScan                 context.CancelFunc
	updates                    <-chan changes.Progress
	spinner                    int
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
	return &model{ctx: ctx, opts: opts, dark: true, picker: opts.Thread == "" && opts.Preferences.LastThread == "", listLoading: true, status: "正在连接 Codex…", collapsed: make(map[string]bool)}
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

func (m *model) openThread(id string) tea.Cmd {
	if m.reader == nil || id == "" {
		return nil
	}
	if m.cancelScan != nil {
		m.cancelScan()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelScan = cancel
	m.generation++
	m.loading, m.picker, m.err, m.status = true, false, "", "正在读取修改记录…"
	m.fileQuery, m.fileCursor, m.fileOffset, m.diffOffset, m.diffColumn = "", 0, 0, 0, 0
	m.session, m.files, m.rows = changes.Session{}, nil, nil
	m.collapsed = make(map[string]bool)
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
		id := m.opts.Thread
		if id == "" {
			id = m.opts.Preferences.LastThread
		}
		if id != "" {
			return m, tea.Batch(m.listCmd(), m.openThread(id))
		}
		return m, m.listCmd()
	case threadsMsg:
		m.listLoading = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.threads = msg.threads
		m.filterThreads()
		if m.session.Thread.ID == "" && !m.loading {
			m.status = "选择会话，查看它改过的文件"
		}
	case scanMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		previousPath := m.selectedPath()
		m.session = msg.progress.Session
		m.filterFiles(previousPath)
		if preferred := m.opts.File; preferred != "" {
			m.selectFile(preferred)
		} else if previousPath == "" {
			m.selectFile(m.opts.Preferences.LastFile)
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
				m.err = fmt.Sprintf("这份历史中找不到文件 %q；可按 / 重新筛选", m.opts.File)
			}
			m.opts.File = ""
			m.opts.Preferences.LastFile = m.selectedPath()
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
		if m.picker {
			m.pickerQuery += inputText(msg.Content)
			m.pickerCursor = 0
			m.filterThreads()
		} else if m.filtering {
			m.fileQuery += inputText(msg.Content)
			m.filterFiles("")
			m.rebuildDiff()
		}
	case tea.MouseClickMsg:
		return m, m.click(msg.Mouse())
	case tea.MouseWheelMsg:
		m.wheel(msg.Mouse())
	}
	return m, nil
}

func (m *model) key(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if key == "ctrl+c" {
		return tea.Quit
	}
	if m.help {
		if key == "esc" || key == "?" || key == "q" {
			m.help = false
		}
		return nil
	}
	if m.picker {
		return m.pickerKey(msg)
	}
	if m.filtering {
		switch key {
		case "enter":
			m.filtering = false
		case "esc":
			m.filtering = false
			m.fileQuery = ""
		case "backspace":
			m.fileQuery = backspace(m.fileQuery)
		case "ctrl+u":
			m.fileQuery = ""
		default:
			m.fileQuery += inputText(msg.Key().Text)
		}
		m.filterFiles("")
		m.rebuildDiff()
		return nil
	}
	switch key {
	case "q":
		return tea.Quit
	case "?":
		m.help = true
	case "s":
		m.picker = true
		m.pickerQuery = ""
		m.filterThreads()
	case "r":
		if m.reader == nil {
			m.listLoading, m.err = true, ""
			return m.Init()
		}
		return m.openThread(m.opts.Preferences.LastThread)
	case "tab", "shift+tab":
		m.focusDiff = !m.focusDiff
		m.rebuildDiff()
	case "/", "f":
		m.filtering = true
		m.focusDiff = false
		m.hideFiles = false
		m.rebuildDiff()
	case "esc":
		m.fileQuery = ""
		m.focusDiff = false
		m.filterFiles(m.selectedPath())
		m.rebuildDiff()
	case "enter":
		m.focusDiff = true
		m.rebuildDiff()
	case "b":
		m.hideFiles = !m.hideFiles
		m.focusDiff = m.hideFiles
		m.rebuildDiff()
	case "w":
		m.opts.Preferences.Wrap = !m.opts.Preferences.Wrap
		m.diffColumn = 0
		m.rebuildDiff()
	case "v":
		m.opts.Preferences.Split = !m.opts.Preferences.Split
		m.rebuildDiff()
	case "t":
		switch m.opts.Preferences.Theme {
		case "auto":
			m.opts.Preferences.Theme = "dark"
		case "dark":
			m.opts.Preferences.Theme = "light"
		default:
			m.opts.Preferences.Theme = "auto"
		}
		m.rebuildDiff()
	case "y":
		if path := m.selectedPath(); path != "" {
			return copyPath(path)
		}
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup", "ctrl+u":
		m.move(-max(1, m.diffHeight()/2))
	case "pgdown", "ctrl+d":
		m.move(max(1, m.diffHeight()/2))
	case "home", "g":
		if m.focusDiff {
			m.diffOffset = 0
		} else {
			m.fileCursor = 0
			m.fileSelectionChanged()
		}
	case "end", "G":
		if m.focusDiff {
			m.diffOffset = len(m.rows)
		} else {
			m.fileCursor = len(m.files) - 1
			m.fileSelectionChanged()
		}
		m.clamp()
	case "left", "h":
		if m.focusDiff && !m.opts.Preferences.Wrap {
			m.diffColumn = max(0, m.diffColumn-8)
			m.rebuildDiff()
		}
	case "right", "l":
		if m.focusDiff && !m.opts.Preferences.Wrap {
			m.diffColumn = min(m.maxColumn(), m.diffColumn+8)
			m.rebuildDiff()
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

func (m *model) pickerKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		if m.session.Thread.ID != "" {
			m.picker = false
		} else {
			m.pickerQuery = ""
			m.filterThreads()
		}
	case "enter":
		if len(m.matchingThreads) > 0 {
			m.opts.Preferences.LastFile = ""
			return m.openThread(m.threads[m.matchingThreads[m.pickerCursor]].ID)
		}
	case "up":
		m.pickerCursor--
	case "down":
		m.pickerCursor++
	case "pgup":
		m.pickerCursor -= max(1, m.pickerPage())
	case "pgdown":
		m.pickerCursor += max(1, m.pickerPage())
	case "home":
		m.pickerCursor = 0
	case "end":
		m.pickerCursor = len(m.matchingThreads) - 1
	case "backspace":
		m.pickerQuery = backspace(m.pickerQuery)
		m.pickerCursor = 0
		m.filterThreads()
	case "ctrl+u":
		m.pickerQuery = ""
		m.pickerCursor = 0
		m.filterThreads()
	case "ctrl+r":
		if m.reader != nil {
			m.listLoading = true
			return m.listCmd()
		}
		return m.Init()
	default:
		m.pickerQuery += inputText(msg.Key().Text)
		m.pickerCursor = 0
		m.filterThreads()
	}
	m.clamp()
	return nil
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

func (m *model) filterThreads() {
	m.matchingThreads = nil
	query := strings.ToLower(strings.TrimSpace(m.pickerQuery))
	for i, thread := range m.threads {
		if strings.Contains(strings.ToLower(thread.Title()+" "+thread.CWD+" "+thread.ID), query) {
			m.matchingThreads = append(m.matchingThreads, i)
		}
	}
	m.clamp()
}

func (m *model) filterFiles(preferred string) {
	m.files = nil
	query := strings.ToLower(strings.TrimSpace(m.fileQuery))
	for i, file := range m.session.Files {
		if strings.Contains(strings.ToLower(file.Path+" "+strings.Join(file.Aliases, " ")), query) {
			m.files = append(m.files, i)
		}
	}
	m.fileCursor = 0
	m.selectFile(preferred)
	m.clamp()
}

func (m *model) selectFile(path string) bool {
	if path == "" {
		return false
	}
	for i, index := range m.files {
		f := m.session.Files[index]
		if f.Path == path || filepath.Base(f.Path) == path {
			m.fileCursor = i
			m.clamp()
			return true
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

func (m *model) fileSelectionChanged() {
	m.clamp()
	m.diffOffset, m.diffColumn = 0, 0
	m.opts.Preferences.LastFile = m.selectedPath()
	m.rebuildDiff()
}

func (m *model) move(delta int) {
	if m.focusDiff {
		m.diffOffset += delta
		m.clamp()
	} else {
		m.fileCursor += delta
		m.fileSelectionChanged()
	}
}

func (m *model) jumpEdit(direction int) {
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
	if len(m.rows) == 0 {
		return
	}
	row := m.rows[min(m.diffOffset, len(m.rows)-1)]
	m.collapsed[row.editID] = !m.collapsed[row.editID]
	m.rebuildDiff()
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
	m.fileCursor = max(0, min(m.fileCursor, len(m.files)-1))
	m.fileOffset = visibleStart(m.fileOffset, m.fileCursor, max(1, (m.bodyHeight()-2)/2), len(m.files))
	m.pickerCursor = max(0, min(m.pickerCursor, len(m.matchingThreads)-1))
	m.pickerOffset = visibleStart(m.pickerOffset, m.pickerCursor, m.pickerPage(), len(m.matchingThreads))
	m.diffOffset = max(0, min(m.diffOffset, max(0, len(m.rows)-m.diffHeight())))
}

func (m *model) bodyHeight() int { return max(1, m.height-5) }
func (m *model) diffHeight() int { return max(1, m.bodyHeight()-2) }
func (m *model) pickerPage() int { return max(1, (m.height-7)/2) }
func (m *model) narrow() bool    { return m.width < 90 }
func (m *model) sidebarWidth() int {
	if m.hideFiles || m.narrow() {
		return 0
	}
	return min(44, max(30, m.width/4))
}
func (m *model) diffWidth() int {
	if w := m.sidebarWidth(); w > 0 {
		return max(1, m.width-w-1)
	}
	return max(1, m.width)
}

func (m *model) click(mouse tea.Mouse) tea.Cmd {
	if mouse.Button != tea.MouseLeft || m.help {
		return nil
	}
	if m.picker {
		index := m.pickerOffset + (mouse.Y-4)/2
		if mouse.Y >= 4 && mouse.Y < m.height-3 && index < len(m.matchingThreads) {
			m.pickerCursor = index
			m.opts.Preferences.LastFile = ""
			return m.openThread(m.threads[m.matchingThreads[index]].ID)
		}
		return nil
	}
	if mouse.Y <= 1 {
		m.picker = true
		m.pickerQuery = ""
		m.filterThreads()
		return nil
	}
	left := m.sidebarWidth() > 0 && mouse.X < m.sidebarWidth() || m.narrow() && !m.focusDiff && !m.hideFiles
	if left && mouse.Y == 4 {
		m.filtering, m.focusDiff = true, false
		return nil
	}
	if mouse.Y < 5 || mouse.Y >= m.height-2 {
		return nil
	}
	if left {
		index := m.fileOffset + (mouse.Y-5)/2
		if index < len(m.files) {
			m.fileCursor, m.focusDiff = index, false
			m.fileSelectionChanged()
			if m.narrow() {
				m.focusDiff = true
				m.rebuildDiff()
			}
		}
	} else {
		m.focusDiff = true
		index := m.diffOffset + mouse.Y - 5
		if index < len(m.rows) && m.rows[index].header {
			id := m.rows[index].editID
			m.collapsed[id] = !m.collapsed[id]
			m.rebuildDiff()
		}
	}
	return nil
}

func (m *model) wheel(mouse tea.Mouse) {
	if m.help {
		return
	}
	delta := 3
	if mouse.Button == tea.MouseWheelUp {
		delta = -3
	} else if mouse.Button != tea.MouseWheelDown {
		return
	}
	if m.picker {
		m.pickerCursor += delta
		m.clamp()
		return
	}
	m.focusDiff = !(m.sidebarWidth() > 0 && mouse.X < m.sidebarWidth() || m.narrow() && !m.focusDiff && !m.hideFiles)
	m.move(delta)
}
