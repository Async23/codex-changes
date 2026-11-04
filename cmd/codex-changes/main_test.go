package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDemoCheckWithoutCodex(t *testing.T) {
	var output bytes.Buffer
	if err := run(context.Background(), []string{"check", "--demo", "--codex-bin", "does-not-exist", "--file", "读出时间.md"}, &output); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Files, Edits, Children int
		SelectedFile           struct{ Added, Removed, Edits int }
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Files != 4 || result.Edits != 7 || result.Children != 1 || result.SelectedFile.Added != 13 || result.SelectedFile.Removed != 1 {
		t.Fatalf("wrong check result: %s", output.String())
	}
	if strings.Contains(output.String(), "读出时间取决于") {
		t.Fatal("check output leaked diff bodies")
	}
}

func TestInvalidCLIInputsFail(t *testing.T) {
	for _, args := range [][]string{{"check"}, {"check", "--demo", "--file", "missing"}, {"unexpected"}} {
		var output bytes.Buffer
		if err := run(context.Background(), args, &output); err == nil {
			t.Fatalf("expected failure for %v", args)
		}
	}
}
