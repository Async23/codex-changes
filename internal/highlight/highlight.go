// Package highlight annotates historical patches without reading current files.
// Analysis is independent of terminal width, theme, and scroll position.
package highlight

import (
	"strconv"
	"strings"
	"time"

	"github.com/Async23/codex-changes/internal/changes"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/rivo/uniseg"
)

type Role uint8

const (
	Plain Role = iota
	Keyword
	Declaration
	String
	Number
	Comment
	Function
	Attribute
	Tag
	Operator
	Type
	RoleCount
)

type Span struct {
	Text    string
	Role    Role
	Changed bool
}

type Line struct {
	changes.Line
	Spans []Span
}

type Pair struct{ Left, Right *Line }

type Diff struct {
	Lines  []Line
	Pairs  []Pair
	Digits int
}

// Analyze gives replacement lines stronger word backgrounds only when most of
// the pair is unchanged. Deleted code stays plain; new/context code is lexed in
// contiguous hunk context so removed lines cannot corrupt the new syntax state.
func Analyze(path string, input []changes.Line) *Diff {
	d := &Diff{Lines: make([]Line, len(input)), Digits: 1}
	indices := make(map[*changes.Line]int, len(input))
	for i, line := range input {
		d.Lines[i] = Line{Line: line}
		d.Digits = max(d.Digits, len(strconv.Itoa(max(line.Old, line.New))))
		indices[&input[i]] = i
	}
	ranges := make([][]interval, len(input))
	// Work is bounded even for generated/minified patches. Text is never dropped.
	deadline := time.Now().Add(150 * time.Millisecond)
	for _, pair := range changes.SideBySide(input) {
		p := Pair{}
		if pair.Left != nil {
			p.Left = &d.Lines[indices[pair.Left]]
		}
		if pair.Right != nil {
			p.Right = &d.Lines[indices[pair.Right]]
		}
		d.Pairs = append(d.Pairs, p)
		if p.Left != nil && p.Right != nil && p.Left.Kind == changes.Removed && p.Right.Kind == changes.Added && time.Now().Before(deadline) {
			ranges[indices[pair.Left]], ranges[indices[pair.Right]] = wordChanges(p.Left.Text, p.Right.Text)
		}
	}
	tokens := syntax(path, input)
	for i := range d.Lines {
		d.Lines[i].Spans = annotate(input[i].Text, tokens[i], ranges[i])
	}
	return d
}

func syntax(path string, lines []changes.Line) [][]chroma.Token {
	result := make([][]chroma.Token, len(lines))
	lexer := lexers.Match(path)
	if lexer == nil {
		// An extensionless executable can carry its language in a shebang. Do
		// not guess from arbitrary fragments or read the file from the disk.
		for _, line := range lines {
			if line.New == 1 && strings.HasPrefix(line.Text, "#!") {
				lexer = lexers.Analyse(line.Text + "\n")
				break
			}
		}
	}
	if lexer == nil {
		return result
	}
	var indices []int
	var code strings.Builder
	flush := func() {
		if code.Len() <= 256*1024 {
			rows := tokenize(lexer, code.String())
			if len(rows) == len(indices) {
				for i, index := range indices {
					result[index] = rows[i]
				}
			}
		}
		indices = nil
		code.Reset()
	}
	for i, line := range lines {
		switch line.Kind {
		case changes.Hunk, changes.Metadata:
			flush()
		case changes.Context, changes.Added:
			indices = append(indices, i)
			code.WriteString(line.Text)
			code.WriteByte('\n')
		}
	}
	flush()
	return result
}

func tokenize(lexer chroma.Lexer, code string) (rows [][]chroma.Token) {
	// Chroma iterators can panic on malformed input; that must only disable
	// optional coloring, never prevent inspecting a historical patch.
	defer func() {
		if recover() != nil {
			rows = nil
		}
	}()
	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	if err != nil {
		return nil
	}
	tokens := iterator.Tokens()
	var text strings.Builder
	for _, token := range tokens {
		text.WriteString(token.Value)
	}
	if text.String() != code {
		return nil
	}
	return chroma.SplitTokensIntoLines(tokens)
}

func role(t chroma.TokenType) Role {
	switch {
	case t == chroma.KeywordDeclaration || t == chroma.KeywordType:
		return Declaration
	case t.InCategory(chroma.Keyword):
		return Keyword
	case t.InSubCategory(chroma.LiteralString):
		return String
	case t.InSubCategory(chroma.LiteralNumber):
		return Number
	case t.InCategory(chroma.Comment):
		return Comment
	case t.InSubCategory(chroma.NameFunction):
		return Function
	case t == chroma.NameAttribute:
		return Attribute
	case t == chroma.NameTag:
		return Tag
	case t.InCategory(chroma.Operator):
		return Operator
	case t == chroma.NameClass || t == chroma.NameBuiltin:
		return Type
	default:
		return Plain
	}
}

func annotate(text string, tokens []chroma.Token, ranges []interval) []Span {
	var out []Span
	tokenIndex, tokenEnd, rangeIndex, spanStart := 0, 0, 0, 0
	if len(tokens) > 0 {
		tokenEnd = len(tokens[0].Value)
	}
	g := uniseg.NewGraphemes(text)
	for g.Next() {
		start, end := g.Positions()
		for tokenIndex < len(tokens) && tokenEnd <= start {
			tokenIndex++
			if tokenIndex < len(tokens) {
				tokenEnd += len(tokens[tokenIndex].Value)
			}
		}
		for rangeIndex < len(ranges) && ranges[rangeIndex].end <= start {
			rangeIndex++
		}
		r := Plain
		if tokenIndex < len(tokens) {
			r = role(tokens[tokenIndex].Type)
		}
		changed := rangeIndex < len(ranges) && ranges[rangeIndex].start < end
		if len(out) > 0 && out[len(out)-1].Role == r && out[len(out)-1].Changed == changed {
			out[len(out)-1].Text = text[spanStart:end]
		} else {
			spanStart = start
			out = append(out, Span{Text: g.Str(), Role: r, Changed: changed})
		}
	}
	return out
}
