package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

const maxResponseBytes = 128 << 20

type reply struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
}

// Client owns one child process. The allowlist deliberately has no resume,
// start, write, review, archive, or model-execution methods.
type Client struct {
	process *exec.Cmd
	input   io.WriteCloser
	cancel  context.CancelFunc
	done    chan struct{}
	closed  sync.Once
	seq     atomic.Int64
	writes  sync.Mutex
	mu      sync.Mutex
	waiters map[int64]chan reply
	err     error
}

func Start(ctx context.Context, binary string) (*Client, error) {
	return start(ctx, binary, []string{"app-server", "--listen", "stdio://"})
}

func start(ctx context.Context, binary string, args []string) (*Client, error) {
	processCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(processCtx, binary, args...)
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = in.Close()
		return nil, err
	}
	// App-server may print private tool/config details; do not persist or echo them.
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		cancel()
		_ = in.Close()
		_ = out.Close()
		return nil, fmt.Errorf("启动 %s app-server 失败：%w", binary, err)
	}
	c := &Client{process: cmd, input: in, cancel: cancel, done: make(chan struct{}), waiters: make(map[int64]chan reply)}
	go c.receive(out)
	initCtx, stop := context.WithTimeout(ctx, 15*time.Second)
	defer stop()
	err = c.call(initCtx, "initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "codex_changes", "title": "Codex Changes", "version": "0.1.0"},
		"capabilities": map[string]bool{"experimentalApi": true},
	}, nil)
	if err == nil {
		err = c.send(map[string]any{"method": "initialized", "params": map[string]any{}})
	}
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("连接 Codex 失败：%w", err)
	}
	return c, nil
}

func (c *Client) receive(out io.Reader) {
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 64<<10), maxResponseBytes)
	var readErr error
	for scanner.Scan() {
		var envelope struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
			readErr = errors.New("Codex 返回了无效 JSON")
			break
		}
		if envelope.ID == nil {
			continue
		}
		if envelope.Method != "" {
			// Unexpected server requests must never be approved by a history reader.
			_ = c.send(map[string]any{"id": *envelope.ID, "error": map[string]any{"code": -32601, "message": "Read-only history viewer"}})
			continue
		}
		var r reply
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			readErr = err
			break
		}
		c.mu.Lock()
		ch := c.waiters[r.ID]
		delete(c.waiters, r.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- r
		}
	}
	if err := scanner.Err(); err != nil {
		readErr = fmt.Errorf("读取 Codex 响应：%w", err)
	}
	c.cancel()
	_ = c.input.Close()
	waitErr := c.process.Wait()
	if readErr == nil {
		readErr = waitErr
	}
	if readErr == nil {
		readErr = errors.New("Codex app-server 已断开")
	}
	c.mu.Lock()
	c.err = readErr
	c.mu.Unlock()
	close(c.done)
}

func (c *Client) send(message any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	c.writes.Lock()
	defer c.writes.Unlock()
	_, err = c.input.Write(append(data, '\n'))
	return err
}

func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	switch method {
	case "initialize", "thread/list", "thread/read", "thread/turns/list":
	default:
		return fmt.Errorf("拒绝非只读方法 %q", method)
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	id := c.seq.Add(1)
	ch := make(chan reply, 1)
	c.mu.Lock()
	c.waiters[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.waiters, id); c.mu.Unlock() }()
	if err := c.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case r := <-ch:
		if r.Error != nil {
			return r.Error
		}
		if result == nil {
			return nil
		}
		if err := json.Unmarshal(r.Result, result); err != nil {
			return fmt.Errorf("解析 %s 响应：%w", method, err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.err
	}
}

func (c *Client) Close() error {
	c.closed.Do(func() { c.cancel(); _ = c.input.Close() })
	<-c.done
	return nil
}

func (c *Client) ListThreads(ctx context.Context, options ListOptions) ([]Thread, error) {
	var threads []Thread
	cursor := ""
	seen := map[string]bool{}
	for {
		params := map[string]any{"limit": 200, "sortKey": "updated_at", "sortDirection": "desc", "useStateDbOnly": true, "archived": options.Archived}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if options.AncestorID != "" {
			params["ancestorThreadId"] = options.AncestorID
			params["sourceKinds"] = []string{"cli", "vscode", "exec", "appServer", "subAgent", "subAgentThreadSpawn", "subAgentOther", "unknown"}
		} else {
			params["sourceKinds"] = []string{"cli", "vscode", "appServer", "exec", "unknown"}
		}
		var page struct {
			Data       []Thread `json:"data"`
			NextCursor string   `json:"nextCursor"`
		}
		if err := c.call(ctx, "thread/list", params, &page); err != nil {
			return nil, err
		}
		for _, thread := range page.Data {
			thread.Archived = options.Archived
			threads = append(threads, thread)
		}
		if page.NextCursor == "" {
			return threads, nil
		}
		if seen[page.NextCursor] {
			return nil, errors.New("Codex 会话分页游标重复")
		}
		seen[page.NextCursor] = true
		cursor = page.NextCursor
	}
}

func (c *Client) ReadThread(ctx context.Context, id string, includeTurns bool) (Thread, error) {
	var result struct {
		Thread Thread `json:"thread"`
	}
	err := c.call(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": includeTurns}, &result)
	if err == nil && result.Thread.ID == "" {
		err = errors.New("Codex 未返回会话 ID")
	}
	return result.Thread, err
}

func (c *Client) ReadTurns(ctx context.Context, id, cursor string) (TurnPage, error) {
	params := map[string]any{"threadId": id, "limit": 30, "sortDirection": "asc", "itemsView": "full"}
	if cursor != "" {
		params["cursor"] = cursor
	}
	var page TurnPage
	err := c.call(ctx, "thread/turns/list", params, &page)
	return page, err
}
