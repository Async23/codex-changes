package changes

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Async23/codex-changes/internal/codex"
)

func patchTurn(id, status string, at int64, changes ...codex.FileChange) codex.Turn {
	item, _ := json.Marshal(codex.Item{ID: id + "-patch", Type: "fileChange", Status: status, Changes: changes})
	return codex.Turn{ID: id, StartedAt: at, ItemsView: "full", Items: []json.RawMessage{item}}
}

func TestAggregateNonGitFilesAndSkipUnsuccessfulEdits(t *testing.T) {
	root := codex.Thread{ID: "root", CWD: "/synthetic/home"}
	b := NewBuilder(root)
	change := codex.FileChange{Path: "notes/中文.md", Kind: codex.ChangeKind{Type: "update"}, Diff: "@@ -1 +1 @@\n-old\n+new\n"}
	for i, status := range []string{"completed", "failed", "declined", "inProgress", "completed"} {
		id := string(rune('a' + i))
		if err := b.AddTurn(root, patchTurn(id, status, int64(i), change), i+1); err != nil {
			t.Fatal(err)
		}
	}
	// A repeated turn must not double count.
	_ = b.AddTurn(root, patchTurn("a", "completed", 0, change), 1)
	s := b.Snapshot()
	if len(s.Files) != 1 || s.Files[0].Path != "/synthetic/home/notes/中文.md" || len(s.Files[0].Edits) != 2 || s.Ignored != 3 || s.TurnsRead != 5 {
		t.Fatalf("bad aggregation: %+v", s)
	}
	if s.Files[0].Added != 2 || s.Files[0].Removed != 2 || s.Files[0].Edits[0].TurnNumber != 5 {
		t.Fatal("totals or newest-first order are wrong")
	}
}

func TestRenameRetainsHistoryAndSnapshotIsStable(t *testing.T) {
	root := codex.Thread{ID: "root", CWD: "/synthetic"}
	b := NewBuilder(root)
	_ = b.AddTurn(root, patchTurn("a", "completed", 1, codex.FileChange{Path: "old.md", Kind: codex.ChangeKind{Type: "add"}, Diff: "hello\n"}), 1)
	before := b.Snapshot()
	_ = b.AddTurn(root, patchTurn("b", "completed", 2, codex.FileChange{Path: "old.md", Kind: codex.ChangeKind{Type: "update", MovePath: "new.md"}, Diff: "@@ -1 +1 @@\n-hello\n+world\n"}), 2)
	after := b.Snapshot()
	if len(after.Files) != 1 || after.Files[0].Path != "/synthetic/new.md" || len(after.Files[0].Edits) != 2 || after.Files[0].Edits[0].From != "/synthetic/old.md" {
		t.Fatalf("rename lost edits: %+v", after)
	}
	if before.Files[0].Path != "/synthetic/old.md" || len(before.Files[0].Edits) != 1 {
		t.Fatal("snapshot mutated during scan")
	}
}

func TestOnlyRelevantItemsAreDecoded(t *testing.T) {
	root := codex.Thread{ID: "root", CWD: "/synthetic"}
	b := NewBuilder(root)
	turn := patchTurn("a", "completed", 1, codex.FileChange{Path: "/outside/project.md", Kind: codex.ChangeKind{Type: "add"}, Diff: "safe\n"})
	turn.Items = append(turn.Items, json.RawMessage(`{"type":"commandExecution","aggregatedOutput":"PRIVATE OUTPUT"}`), json.RawMessage(`{"type":"userMessage","content":[{"type":"text","text":"Explain this edit"}]}`))
	if err := b.AddTurn(root, turn, 1); err != nil {
		t.Fatal(err)
	}
	s := b.Snapshot()
	if s.Files[0].Path != "/outside/project.md" || s.Files[0].Edits[0].Prompt != "Explain this edit" {
		t.Fatal("outside-cwd file or prompt missing")
	}
	data, _ := json.Marshal(s)
	if strings.Contains(string(data), "PRIVATE OUTPUT") {
		t.Fatal("command output was retained in the file history")
	}
}

func TestChildRenameUsesChronologyInsteadOfScanOrder(t *testing.T) {
	root := codex.Thread{ID: "root", CWD: "/synthetic"}
	child := codex.Thread{ID: "child", CWD: root.CWD}
	b := NewBuilder(root)
	_ = b.AddTurn(root, patchTurn("first", "completed", 10, codex.FileChange{Path: "old.md", Kind: codex.ChangeKind{Type: "add"}, Diff: "original\n"}), 1)
	_ = b.AddTurn(root, patchTurn("recreated", "completed", 30, codex.FileChange{Path: "old.md", Kind: codex.ChangeKind{Type: "add"}, Diff: "different file\n"}), 2)
	_ = b.AddTurn(child, patchTurn("rename", "completed", 20, codex.FileChange{Path: "old.md", Kind: codex.ChangeKind{Type: "update", MovePath: "new.md"}}), 1)
	s := b.Snapshot()
	if len(s.Files) != 2 {
		t.Fatalf("recreated path was merged away: %+v", s.Files)
	}
	for _, f := range s.Files {
		if f.Path == "/synthetic/old.md" && (len(f.Edits) != 1 || f.Edits[0].Lines[0].Text != "different file") {
			t.Fatal("old path history is incorrect")
		}
		if f.Path == "/synthetic/new.md" && len(f.Edits) != 2 {
			t.Fatal("renamed file lost its history")
		}
	}
}
