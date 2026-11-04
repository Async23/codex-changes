// Package codex reads history through the public Codex app-server protocol.
package codex

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Thread struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Preview     string          `json:"preview"`
	CWD         string          `json:"cwd"`
	Source      json.RawMessage `json:"source"`
	HistoryMode string          `json:"historyMode"`
	ParentID    string          `json:"parentThreadId"`
	AgentName   string          `json:"agentNickname"`
	CreatedAt   int64           `json:"createdAt"`
	UpdatedAt   int64           `json:"updatedAt"`
	Turns       []Turn          `json:"turns"`
	Archived    bool            `json:"-"`
}

func (t Thread) Title() string {
	for _, value := range []string{t.Name, t.Preview, t.AgentName, t.ID} {
		if value = strings.TrimSpace(value); value != "" {
			return strings.SplitN(value, "\n", 2)[0]
		}
	}
	return "未命名会话"
}

type Turn struct {
	ID          string            `json:"id"`
	Status      string            `json:"status"`
	StartedAt   int64             `json:"startedAt"`
	CompletedAt int64             `json:"completedAt"`
	ItemsView   string            `json:"itemsView"`
	Items       []json.RawMessage `json:"items"`
}

type TurnPage struct {
	Data       []Turn `json:"data"`
	NextCursor string `json:"nextCursor"`
}

type ListOptions struct {
	Archived   bool
	AncestorID string
}

type Item struct {
	ID      string       `json:"id"`
	Type    string       `json:"type"`
	Status  string       `json:"status"`
	Changes []FileChange `json:"changes"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

type FileChange struct {
	Path string     `json:"path"`
	Kind ChangeKind `json:"kind"`
	Diff string     `json:"diff"`
}

// ChangeKind accepts the tagged protocol object and older string representations.
type ChangeKind struct {
	Type     string `json:"type"`
	MovePath string `json:"move_path"`
}

func (k *ChangeKind) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &k.Type)
	}
	type plain ChangeKind
	return json.Unmarshal(data, (*plain)(k))
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("Codex (%d): %s", e.Code, e.Message) }
