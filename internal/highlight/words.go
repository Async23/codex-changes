package highlight

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
	"github.com/sergi/go-diff/diffmatchpatch"
)

type interval struct{ start, end int }

// Word and whitespace runs retain exact spelling and spacing. CJK graphemes
// are separate tokens so a small Chinese edit doesn't highlight a full sentence.
func words(text string) []string {
	var tokens []string
	lastClass, start, end := 0, 0, 0
	g := uniseg.NewGraphemes(text)
	for g.Next() {
		a, b := g.Positions()
		r, _ := utf8.DecodeRuneInString(g.Str())
		class := 0
		switch {
		case unicode.IsSpace(r):
			class = 1
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
		case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_':
			class = 2
		}
		if a > start && (class == 0 || class != lastClass) {
			tokens = append(tokens, text[start:a])
			start = a
		}
		lastClass, end = class, b
	}
	if end > start {
		tokens = append(tokens, text[start:end])
	}
	return tokens
}

func wordChanges(before, after string) ([]interval, []interval) {
	if before == after || len(before)+len(after) > 16*1024 {
		return nil, nil
	}
	// Encode tokens as runes for the library's Myers diff, then map offsets
	// back to original UTF-8 bytes. Encoded text is never sent to the terminal.
	dictionary := []string{""}
	ids := make(map[string]rune)
	encode := func(text string) []rune {
		var result []rune
		for _, word := range words(text) {
			id, ok := ids[word]
			if !ok {
				id = rune(len(dictionary))
				ids[word] = id
				dictionary = append(dictionary, word)
			}
			result = append(result, id)
		}
		return result
	}
	oldTokens, newTokens := encode(before), encode(after)
	if len(oldTokens)+len(newTokens) > 4096 {
		return nil, nil
	}
	dmp := diffmatchpatch.New()
	dmp.DiffTimeout = 10 * time.Millisecond
	var oldRanges, newRanges []interval
	oldOffset, newOffset, changed := 0, 0, 0
	for _, delta := range dmp.DiffMainRunes(oldTokens, newTokens, false) {
		var text strings.Builder
		for _, id := range delta.Text {
			text.WriteString(dictionary[id])
		}
		size := text.Len()
		switch delta.Type {
		case diffmatchpatch.DiffDelete:
			oldRanges = append(oldRanges, interval{oldOffset, oldOffset + size})
			oldOffset += size
			changed += utf8.RuneCountInString(text.String())
		case diffmatchpatch.DiffInsert:
			newRanges = append(newRanges, interval{newOffset, newOffset + size})
			newOffset += size
			changed += utf8.RuneCountInString(text.String())
		default:
			oldOffset += size
			newOffset += size
		}
	}
	if float64(changed) > 0.4*float64(utf8.RuneCountInString(before)+utf8.RuneCountInString(after)) {
		return nil, nil
	}
	return oldRanges, newRanges
}
