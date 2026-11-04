package tui

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

type clipboardMsg struct {
	text   string
	native bool
	err    error
}

func copyPath(text string) tea.Cmd {
	return func() tea.Msg {
		// Native copying on macOS also works when tmux blocks application OSC 52.
		if runtime.GOOS == "darwin" {
			if binary, err := exec.LookPath("pbcopy"); err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, binary)
				command.Stdin = strings.NewReader(text)
				return clipboardMsg{native: true, err: command.Run()}
			}
		}
		return clipboardMsg{text: text}
	}
}
