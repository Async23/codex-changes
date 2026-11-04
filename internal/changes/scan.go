package changes

import (
	"context"
	"errors"
	"fmt"

	"github.com/Async23/codex-changes/internal/codex"
)

type Reader interface {
	ListThreads(context.Context, codex.ListOptions) ([]codex.Thread, error)
	ReadThread(context.Context, string, bool) (codex.Thread, error)
	ReadTurns(context.Context, string, string) (codex.TurnPage, error)
}

type Progress struct {
	Session Session
	Done    bool
	Err     error
}

func Scan(ctx context.Context, reader Reader, id string, children bool, emit func(Progress)) {
	root, err := reader.ReadThread(ctx, id, false)
	if err != nil {
		emit(Progress{Done: true, Err: err})
		return
	}
	b := NewBuilder(root)
	threads := []codex.Thread{root}
	if children {
		var candidates []codex.Thread
		for _, archived := range []bool{false, true} {
			descendants, listErr := reader.ListThreads(ctx, codex.ListOptions{AncestorID: id, Archived: archived})
			if listErr != nil {
				if ctx.Err() != nil {
					emit(Progress{Done: true, Err: ctx.Err()})
					return
				}
				b.session.Warnings = append(b.session.Warnings, "子会话未完整读取："+listErr.Error())
				continue
			}
			candidates = append(candidates, descendants...)
		}
		linked, unlinked := descendantsOf(root.ID, candidates)
		threads = append(threads, linked...)
		if unlinked {
			b.session.Warnings = append(b.session.Warnings, "Codex 返回了无法关联到当前会话的子会话，已跳过；子会话汇总可能不完整")
		}
	}
	b.session.Children = len(threads) - 1
	for index, thread := range threads {
		err := scanThread(ctx, reader, thread, b, func() { emit(Progress{Session: b.Snapshot()}) })
		if err != nil {
			if index == 0 || ctx.Err() != nil {
				emit(Progress{Session: b.Snapshot(), Done: true, Err: err})
				return
			}
			b.session.Warnings = append(b.session.Warnings, fmt.Sprintf("子会话 %s 未完整读取：%v", thread.ID, err))
		}
	}
	emit(Progress{Session: b.Snapshot(), Done: true})
}

// Older servers can silently ignore a new filter. Verify ancestry before
// scanning so an ignored ancestorThreadId never reads unrelated histories.
func descendantsOf(root string, candidates []codex.Thread) ([]codex.Thread, bool) {
	seen := map[string]bool{root: true}
	var linked []codex.Thread
	for {
		changed := false
		for _, thread := range candidates {
			if thread.ID != "" && !seen[thread.ID] && seen[thread.ParentID] {
				seen[thread.ID] = true
				linked = append(linked, thread)
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	for _, thread := range candidates {
		if !seen[thread.ID] {
			return linked, true
		}
	}
	return linked, false
}

func scanThread(ctx context.Context, reader Reader, thread codex.Thread, b *Builder, emit func()) error {
	if thread.HistoryMode == "legacy" {
		return scanLegacy(ctx, reader, thread.ID, b, emit)
	}
	cursor, number := "", 0
	seenCursors := make(map[string]bool)
	for {
		page, err := reader.ReadTurns(ctx, thread.ID, cursor)
		if err != nil {
			var rpc *codex.RPCError
			if cursor == "" && errors.As(err, &rpc) && rpc.Code == -32601 {
				return scanLegacy(ctx, reader, thread.ID, b, emit)
			}
			return err
		}
		for _, turn := range page.Data {
			if turn.ItemsView != "" && turn.ItemsView != "full" {
				return errors.New("Codex 只返回了摘要，无法完整统计文件修改")
			}
			number++
			if err := b.AddTurn(thread, turn, number); err != nil {
				return err
			}
		}
		emit()
		if page.NextCursor == "" {
			return nil
		}
		if seenCursors[page.NextCursor] {
			return errors.New("Codex 修改历史分页游标重复")
		}
		seenCursors[page.NextCursor] = true
		cursor = page.NextCursor
	}
}

func scanLegacy(ctx context.Context, reader Reader, id string, b *Builder, emit func()) error {
	thread, err := reader.ReadThread(ctx, id, true)
	if err != nil {
		return err
	}
	for number, turn := range thread.Turns {
		if err := ctx.Err(); err != nil {
			return err
		}
		if turn.ItemsView != "" && turn.ItemsView != "full" {
			return errors.New("Codex 只返回了摘要，无法完整统计文件修改")
		}
		if err := b.AddTurn(thread, turn, number+1); err != nil {
			return err
		}
	}
	emit()
	return nil
}
