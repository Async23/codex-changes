package highlight

import (
	"strings"
	"testing"

	"github.com/Async23/codex-changes/internal/changes"
	"github.com/rivo/uniseg"
)

func spanText(spans []Span, changedOnly bool) string {
	var text strings.Builder
	for _, span := range spans {
		if !changedOnly || span.Changed {
			text.WriteString(span.Text)
		}
	}
	return text.String()
}

func TestWordHighlightKeepsExactTextAndWholeWords(t *testing.T) {
	for _, tc := range []struct{ before, after, oldWords, newWords string }{
		{`const filename = "draft.svg";`, `const filename = "final.svg";`, "draft", "final"},
		{`<text x="120" y="104">说明</text>`, `<text x="120" y="128">说明</text>`, "104", "128"},
		{"同步复制，可以接收连接", "同步复制，不能接收连接", "可以", "不能"},
		{"return  frame.width", "return\tframe.width", "  ", "\t"},
		{"const label = 'é 👩🏽‍💻';", "const label = 'è 👩🏻‍💻';", "é👩🏽‍💻", "è👩🏻‍💻"},
	} {
		t.Run(tc.before, func(t *testing.T) {
			input := []changes.Line{{Kind: changes.Removed, Text: tc.before, Old: 1234}, {Kind: changes.Added, Text: tc.after, New: 1234}}
			d := Analyze("sample.ts", input)
			if d.Digits != 4 || len(d.Pairs) != 1 {
				t.Fatalf("unexpected layout metadata: %+v", d)
			}
			for i, want := range []string{tc.oldWords, tc.newWords} {
				if got := spanText(d.Lines[i].Spans, true); got != want {
					t.Errorf("changed words: %q, want %q", got, want)
				}
				if got := spanText(d.Lines[i].Spans, false); got != input[i].Text {
					t.Fatalf("content changed: %q", got)
				}
				boundaries := map[int]bool{0: true}
				g := uniseg.NewGraphemes(input[i].Text)
				for g.Next() {
					_, end := g.Positions()
					boundaries[end] = true
				}
				offset := 0
				for _, span := range d.Lines[i].Spans {
					offset += len(span.Text)
					if !boundaries[offset] {
						t.Fatalf("style splits a grapheme at byte %d", offset)
					}
				}
			}
		})
	}
}

func TestMajorChangesAndHugeLinesStayPlain(t *testing.T) {
	for _, pair := range [][2]string{{"read_cache()", "send_network_request()"}, {strings.Repeat("a ", 10000), strings.Repeat("b ", 10000)}} {
		d := Analyze("unknown", []changes.Line{{Kind: changes.Removed, Text: pair[0]}, {Kind: changes.Added, Text: pair[1]}})
		for i, line := range d.Lines {
			if spanText(line.Spans, true) != "" || spanText(line.Spans, false) != pair[i] {
				t.Fatal("fallback lost content or highlighted most of the line")
			}
		}
	}
}

func TestSyntaxUsesNewHunkContextAndNeverDeletedCode(t *testing.T) {
	lines, _, _ := changes.ParseDiff("update", "@@ -10,5 +10,5 @@\n const note = `\n-old text\n+new text\n still a string\n `;\n const count = 42;\n@@ -100,1 +100,1 @@\n-console.log('old');\n+console.log('new');\n")
	d := Analyze("sample.ts", lines)
	for _, line := range d.Lines {
		if spanText(line.Spans, false) != line.Text {
			t.Fatalf("syntax changed text: %q", line.Text)
		}
		if line.Kind == changes.Removed {
			for _, span := range line.Spans {
				if span.Role != Plain {
					t.Fatal("deleted syntax distracts from the new code")
				}
			}
		}
		if line.Text == "still a string" {
			if len(line.Spans) != 1 || line.Spans[0].Role != String {
				t.Fatalf("multiline string lost syntax state: %+v", line.Spans)
			}
		}
	}
	for _, tc := range []struct {
		path, text string
		role       Role
	}{
		{"test.svg", `<text x="120">中文</text>`, Attribute},
		{"test.py", "return 42", Keyword},
		{"test.go", "func main() {}", Declaration},
		{"settings.json", `{"enabled": true}`, Keyword},
		{"Dockerfile", "FROM alpine:3.22", Keyword},
	} {
		d := Analyze(tc.path, []changes.Line{{Kind: changes.Added, Text: tc.text, New: 1}})
		found := false
		for _, span := range d.Lines[0].Spans {
			found = found || span.Role == tc.role
		}
		if !found {
			t.Errorf("%s syntax missing: %+v", tc.path, d.Lines[0].Spans)
		}
	}
}

func TestMissingLinesDoNotReceiveInventedChanges(t *testing.T) {
	input, _, _ := changes.ParseDiff("update", "@@ -3,1 +3,2 @@\n-const size = 10;\n+const size = 12;\n+const label = 'new';\n")
	d := Analyze("test.ts", input)
	if d.Pairs[2].Left != nil || spanText(d.Pairs[2].Right.Spans, true) != "" {
		t.Fatal("unpaired addition should only have a line background")
	}
}

func BenchmarkAnalyze(b *testing.B) {
	var lines []changes.Line
	for i := range 100 {
		lines = append(lines, changes.Line{Kind: changes.Removed, Old: i + 1, Text: `  <text x="120" y="104">说明文字</text>`})
		lines = append(lines, changes.Line{Kind: changes.Added, New: i + 1, Text: `  <text x="120" y="128">更新文字</text>`})
	}
	for b.Loop() {
		Analyze("diagram.svg", lines)
	}
}
