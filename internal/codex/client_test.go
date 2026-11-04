package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

func TestProtocolHelper(t *testing.T) {
	if os.Getenv("CODEX_CHANGES_PROTOCOL_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	initialized := false
	for scanner.Scan() {
		var request struct {
			ID     int64          `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			os.Exit(2)
		}
		if request.Method == "initialized" {
			initialized = true
			continue
		}
		if request.Method != "initialize" && !initialized {
			os.Exit(3)
		}
		var result any = map[string]any{}
		switch request.Method {
		case "initialize":
		case "thread/list":
			if request.Params["useStateDbOnly"] != true {
				os.Exit(4)
			}
			if request.Params["cursor"] == nil {
				result = map[string]any{"data": []Thread{{ID: "one"}}, "nextCursor": "page-two"}
			} else {
				result = map[string]any{"data": []Thread{{ID: "two"}}}
			}
		case "thread/read":
			id, _ := request.Params["threadId"].(string)
			if id == "hang" {
				continue
			}
			if id == "broken" {
				fmt.Fprintln(os.Stdout, "{not-json")
				continue
			}
			result = map[string]any{"thread": Thread{ID: id, CWD: "/synthetic/project"}}
		case "thread/turns/list":
			if request.Params["itemsView"] != "full" || request.Params["sortDirection"] != "asc" {
				os.Exit(5)
			}
			result = TurnPage{Data: []Turn{{ID: "turn", ItemsView: "full"}}}
		default:
			os.Exit(6)
		}
		_ = encoder.Encode(map[string]any{"method": "irrelevant/notification", "params": map[string]string{"type": "ignored"}})
		_ = encoder.Encode(map[string]any{"id": request.ID, "result": result})
	}
	os.Exit(0)
}

func helperClient(t *testing.T) *Client {
	t.Helper()
	t.Setenv("CODEX_CHANGES_PROTOCOL_HELPER", "1")
	c, err := start(context.Background(), os.Args[0], []string{"-test.run=^TestProtocolHelper$"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestProtocolPaginationAndReadOnlyAllowlist(t *testing.T) {
	c := helperClient(t)
	threads, err := c.ListThreads(context.Background(), ListOptions{})
	if err != nil || len(threads) != 2 || threads[1].ID != "two" {
		t.Fatalf("pages: %+v %v", threads, err)
	}
	page, err := c.ReadTurns(context.Background(), "one", "")
	if err != nil || len(page.Data) != 1 {
		t.Fatalf("turns: %+v %v", page, err)
	}
	for _, method := range []string{"thread/resume", "thread/start", "turn/start", "thread/delete"} {
		if err := c.call(context.Background(), method, map[string]any{}, nil); err == nil {
			t.Fatalf("allowed mutation %s", method)
		}
	}
}

func TestCancellationAndConcurrentRequests(t *testing.T) {
	c := helperClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := c.ReadThread(ctx, "hang", false)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	c.mu.Lock()
	remaining := len(c.waiters)
	c.mu.Unlock()
	if remaining != 0 {
		t.Fatal("cancelled request leaked")
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("thread-%d", i)
			thread, err := c.ReadThread(context.Background(), id, false)
			if err != nil || thread.ID != id {
				t.Errorf("crossed reply: %+v %v", thread, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestInvalidServerOutputFailsPendingReads(t *testing.T) {
	c := helperClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c.ReadThread(ctx, "broken", false); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("invalid output was not reported: %v", err)
	}
}

func TestChangeKindAcceptsTaggedAndLegacyForms(t *testing.T) {
	for _, raw := range []string{`"update"`, `{"type":"update","move_path":"renamed.md"}`} {
		var kind ChangeKind
		if err := json.Unmarshal([]byte(raw), &kind); err != nil || kind.Type != "update" {
			t.Fatalf("kind: %+v %v", kind, err)
		}
	}
}
