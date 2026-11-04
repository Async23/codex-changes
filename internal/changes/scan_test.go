package changes

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Async23/codex-changes/internal/codex"
)

type fakeReader struct {
	root        codex.Thread
	pages       map[string]codex.TurnPage
	children    []codex.Thread
	pageErr     error
	readIDs     []string
	legacyReads int
}

func (f *fakeReader) ListThreads(_ context.Context, options codex.ListOptions) ([]codex.Thread, error) {
	if options.Archived {
		return nil, nil
	}
	return f.children, nil
}
func (f *fakeReader) ReadThread(_ context.Context, id string, full bool) (codex.Thread, error) {
	f.readIDs = append(f.readIDs, id)
	if full {
		f.legacyReads++
	}
	if id == f.root.ID {
		return f.root, nil
	}
	for _, child := range f.children {
		if child.ID == id {
			return child, nil
		}
	}
	return codex.Thread{}, fmt.Errorf("unknown thread %s", id)
}
func (f *fakeReader) ReadTurns(_ context.Context, id, cursor string) (codex.TurnPage, error) {
	f.readIDs = append(f.readIDs, id)
	if f.pageErr != nil {
		return codex.TurnPage{}, f.pageErr
	}
	return f.pages[cursor], nil
}

func runScan(reader Reader, children bool) Progress {
	var final Progress
	Scan(context.Background(), reader, "root", children, func(p Progress) {
		if p.Done {
			final = p
		}
	})
	return final
}

func TestScanReadsAllPagesWithFullItems(t *testing.T) {
	f := &fakeReader{root: codex.Thread{ID: "root", CWD: "/synthetic", HistoryMode: "paginated"}, pages: map[string]codex.TurnPage{}}
	change := codex.FileChange{Path: "note.md", Kind: codex.ChangeKind{Type: "add"}, Diff: "text\n"}
	f.pages[""] = codex.TurnPage{Data: []codex.Turn{patchTurn("first", "completed", 1, change)}, NextCursor: "next"}
	f.pages["next"] = codex.TurnPage{Data: []codex.Turn{patchTurn("second", "completed", 2, change)}}
	p := runScan(f, false)
	if p.Err != nil || p.Session.TurnsRead != 2 || p.Session.EditCount() != 2 || f.legacyReads != 0 {
		t.Fatalf("incomplete scan: %+v", p)
	}
	f.pages["next"] = codex.TurnPage{NextCursor: "next"}
	if p = runScan(f, false); p.Err == nil {
		t.Fatal("repeated cursor must fail instead of looping")
	}
	f.pages[""] = codex.TurnPage{Data: []codex.Turn{{ID: "summary", ItemsView: "summary"}}}
	if p = runScan(f, false); p.Err == nil {
		t.Fatal("summary items must not look complete")
	}
}

func TestLegacyAndMethodMissingFallback(t *testing.T) {
	f := &fakeReader{root: codex.Thread{ID: "root", CWD: "/synthetic", HistoryMode: "legacy", Turns: []codex.Turn{{ID: "turn"}}}}
	if p := runScan(f, false); p.Err != nil || f.legacyReads != 1 {
		t.Fatalf("legacy scan: %+v", p)
	}
	f.root.HistoryMode = ""
	f.legacyReads = 0
	f.pageErr = &codex.RPCError{Code: -32601, Message: "method missing"}
	if p := runScan(f, false); p.Err != nil || f.legacyReads != 1 {
		t.Fatalf("fallback: %+v", p)
	}
	f.legacyReads = 0
	f.pageErr = errors.New("transport failed")
	if p := runScan(f, false); p.Err == nil || f.legacyReads != 0 {
		t.Fatal("transport errors must not be hidden by fallback")
	}
}

func TestDescendantsCannotIncludeUnrelatedThreads(t *testing.T) {
	f := &fakeReader{root: codex.Thread{ID: "root", HistoryMode: "legacy"}, children: []codex.Thread{
		{ID: "unrelated", HistoryMode: "legacy"},
		{ID: "grandchild", ParentID: "child", HistoryMode: "legacy"},
		{ID: "child", ParentID: "root", HistoryMode: "legacy"},
	}}
	p := runScan(f, true)
	if p.Err != nil || p.Session.Children != 2 || len(p.Session.Warnings) == 0 {
		t.Fatalf("ancestry not checked: %+v", p)
	}
	for _, id := range f.readIDs {
		if id == "unrelated" {
			t.Fatal("read an unrelated history")
		}
	}
}

func TestDemoCombinesChildEditsIntoParentFile(t *testing.T) {
	var final Progress
	Scan(context.Background(), codex.NewDemo(), "demo-camera", true, func(p Progress) {
		if p.Done {
			final = p
		}
	})
	if final.Err != nil || final.Session.Children != 1 || len(final.Session.Files) != 4 || final.Session.EditCount() != 7 {
		t.Fatalf("demo scan: %+v", final)
	}
	for _, file := range final.Session.Files {
		if file.Path == "/demo/camera/export/settings.json" && len(file.Edits) != 2 {
			t.Fatal("child edit lost")
		}
	}
}
