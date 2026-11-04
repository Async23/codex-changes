package changes

import (
	"strings"
	"testing"
)

func TestDiffKindsAndLineNumbers(t *testing.T) {
	tests := []struct {
		name, kind, body string
		added, removed   int
	}{
		{"add is full content", "add", "+a literal plus\n\nlast\n", 3, 0},
		{"delete is full content", "delete", "old\n", 0, 1},
		{"empty file", "add", "", 0, 0},
		{"multiple hunks and headers", "update", "--- a/file\n+++ b/file\n@@ -2,2 +2,3 @@\n same\n-old\n+++literal\n+new\n@@ -9 +10 @@\n-x\n+y\n\\ No newline at end of file\n", 3, 2},
		{"CRLF", "update", "@@ -1 +1 @@\r\n-old\r\n+new\r\n", 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, added, removed := ParseDiff(tt.kind, tt.body)
			if added != tt.added || removed != tt.removed {
				t.Fatalf("counts +%d -%d, want +%d -%d", added, removed, tt.added, tt.removed)
			}
		})
	}
	lines, _, _ := ParseDiff("update", "@@ -42,2 +42,3 @@\n context\n-old\n+new\n+more\n")
	if lines[1].Old != 42 || lines[1].New != 42 || lines[2].Old != 43 || lines[3].New != 43 || lines[4].New != 44 {
		t.Fatalf("wrong numbers: %#v", lines)
	}
	if lines[2].Text != "old" {
		t.Fatal("patch prefix leaked into content")
	}
}

func TestTerminalControlsCannotEscapeHistory(t *testing.T) {
	input := "正常\x1b[31mred\x1b[0m\x1b]52;c;YWJj\x07\x1b[2J\r\x08\n下一行\t内容"
	got := SafeText(input)
	if strings.ContainsAny(got, "\x1b\x07\x08\r") || strings.Contains(got, "YWJj") {
		t.Fatalf("unsafe output %q", got)
	}
	if !strings.Contains(got, "\n下一行\t内容") {
		t.Fatalf("lost readable text: %q", got)
	}
}

func TestSplitAlignsUnequalReplacementBlocks(t *testing.T) {
	lines, _, _ := ParseDiff("update", "@@ -1,2 +1,3 @@\n-old\n+one\n+two\n context\n")
	pairs := SideBySide(lines)
	if len(pairs) != 4 || pairs[1].Left.Text != "old" || pairs[1].Right.Text != "one" || pairs[2].Left != nil || pairs[2].Right.Text != "two" {
		t.Fatalf("bad pairing: %#v", pairs)
	}
}
