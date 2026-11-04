// Package changes groups persisted, successful edits by file without Git or
// reading the user's current files. It cannot reconstruct unrecorded writes.
package changes

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Async23/codex-changes/internal/codex"
)

type Edit struct {
	ID         string `json:"id"`
	ThreadID   string `json:"threadId"`
	Source     string `json:"source"`
	TurnID     string `json:"turnId"`
	TurnNumber int    `json:"turnNumber"`
	Time       int64  `json:"time"`
	Prompt     string `json:"prompt"`
	Kind       string `json:"kind"`
	From       string `json:"from,omitempty"`
	Lines      []Line `json:"lines"`
	Added      int    `json:"added"`
	Removed    int    `json:"removed"`
	sequence   int
}

type File struct {
	Path    string   `json:"path"`
	Aliases []string `json:"aliases,omitempty"`
	Edits   []Edit   `json:"edits"`
	Added   int      `json:"added"`
	Removed int      `json:"removed"`
}

type Session struct {
	Thread    codex.Thread `json:"thread"`
	Files     []File       `json:"files"`
	TurnsRead int          `json:"turnsRead"`
	Children  int          `json:"children"`
	Ignored   int          `json:"ignored"`
	Warnings  []string     `json:"warnings,omitempty"`
}

func (s Session) EditCount() int {
	n := 0
	for _, file := range s.Files {
		n += len(file.Edits)
	}
	return n
}

type Builder struct {
	session  Session
	records  []record
	seen     map[string]bool
	sequence int
}

type record struct {
	path        string
	destination string
	edit        Edit
}

func NewBuilder(root codex.Thread) *Builder {
	root.Turns = nil
	return &Builder{session: Session{Thread: root}, seen: make(map[string]bool)}
}

func absolute(path, cwd string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(cwd, path))
}

func (b *Builder) AddTurn(thread codex.Thread, turn codex.Turn, number int) error {
	turnKey := thread.ID + ":" + turn.ID
	if b.seen[turnKey] {
		return nil
	}
	b.seen[turnKey] = true
	b.session.TurnsRead++
	var items []codex.Item
	var prompt string
	for _, raw := range turn.Items {
		var header struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			return fmt.Errorf("解析历史条目：%w", err)
		}
		if header.Type != "fileChange" && header.Type != "userMessage" {
			continue
		}
		var item codex.Item
		if err := json.Unmarshal(raw, &item); err != nil {
			return fmt.Errorf("解析 %s：%w", header.Type, err)
		}
		if item.Type == "userMessage" && prompt == "" {
			for _, part := range item.Content {
				if part.Type == "text" {
					prompt += part.Text + " "
				}
			}
			prompt = OneLine(prompt)
			if chars := []rune(prompt); len(chars) > 260 {
				prompt = string(chars[:260]) + "…"
			}
		}
		if item.Type == "fileChange" {
			items = append(items, item)
		}
	}
	for itemNumber, item := range items {
		if item.Status != "completed" {
			b.session.Ignored++
			continue
		}
		for changeNumber, change := range item.Changes {
			if strings.TrimSpace(change.Path) == "" {
				b.session.Warnings = append(b.session.Warnings, "一条修改记录缺少路径，已跳过")
				continue
			}
			path := absolute(change.Path, thread.CWD)
			from, destination := "", ""
			if change.Kind.MovePath != "" {
				destination = absolute(change.Kind.MovePath, thread.CWD)
				if destination != path {
					from = path
				}
			}
			lines, added, removed := ParseDiff(change.Kind.Type, change.Diff)
			stamp := turn.StartedAt
			if stamp == 0 {
				stamp = turn.CompletedAt
			}
			b.sequence++
			b.records = append(b.records, record{path: path, destination: destination, edit: Edit{
				ID: fmt.Sprintf("%s:%s:%d:%d", turnKey, item.ID, itemNumber, changeNumber), ThreadID: thread.ID,
				Source: thread.Title(), TurnID: turn.ID, TurnNumber: number, Time: stamp,
				Prompt: prompt, Kind: change.Kind.Type, From: from, Lines: lines, Added: added, Removed: removed, sequence: b.sequence,
			}})
		}
	}
	return nil
}

// Snapshot copies mutable slices. Callers may render an earlier snapshot while
// the reader continues loading more pages in another goroutine.
func (b *Builder) Snapshot() Session {
	snapshot := b.session
	snapshot.Warnings = append([]string(nil), b.session.Warnings...)
	// Parent and child histories arrive separately. Apply renames in timestamp
	// order so a later recreation of an old pathname stays a separate file.
	records := append([]record(nil), b.records...)
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].edit.Time != records[j].edit.Time {
			return records[i].edit.Time < records[j].edit.Time
		}
		return records[i].edit.sequence < records[j].edit.sequence
	})
	files := make(map[string]*File)
	for _, r := range records {
		file := files[r.path]
		if file == nil {
			file = &File{Path: r.path}
			files[r.path] = file
		}
		if r.destination != "" && r.destination != r.path {
			if target := files[r.destination]; target != nil && target != file {
				file.Edits = append(file.Edits, target.Edits...)
				file.Aliases = append(file.Aliases, target.Aliases...)
				file.Added += target.Added
				file.Removed += target.Removed
			}
			delete(files, r.path)
			file.Path = r.destination
			file.Aliases = append(file.Aliases, r.path)
			files[r.destination] = file
		}
		file.Edits = append(file.Edits, r.edit)
		file.Added += r.edit.Added
		file.Removed += r.edit.Removed
	}
	snapshot.Files = make([]File, 0, len(files))
	for _, file := range files {
		copy := *file
		copy.Edits = append([]Edit(nil), file.Edits...)
		copy.Aliases = append([]string(nil), file.Aliases...)
		sort.SliceStable(copy.Edits, func(i, j int) bool {
			if copy.Edits[i].Time != copy.Edits[j].Time {
				return copy.Edits[i].Time > copy.Edits[j].Time
			}
			return copy.Edits[i].sequence > copy.Edits[j].sequence
		})
		snapshot.Files = append(snapshot.Files, copy)
	}
	sort.Slice(snapshot.Files, func(i, j int) bool { return snapshot.Files[i].Path < snapshot.Files[j].Path })
	return snapshot
}
