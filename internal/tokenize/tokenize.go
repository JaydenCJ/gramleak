// Package tokenize turns raw document text into a normalized token stream
// that keeps byte offsets, so every shingle match can be mapped back to the
// exact source span it came from and quoted as evidence.
package tokenize

import (
	"strings"
	"unicode"
)

// Token is a single normalized word with its byte span in the original text.
// Start/End index the ORIGINAL string, not the normalized form, so callers
// can slice the raw document to display matches verbatim.
type Token struct {
	Text  string // normalized form used for hashing
	Start int    // byte offset of the first byte in the original text
	End   int    // byte offset one past the last byte in the original text
}

// Options control normalization. The zero value is the gramleak default:
// case-folded, digits kept verbatim.
type Options struct {
	// CaseSensitive disables Unicode case folding. Off by default because
	// contaminated text is routinely re-cased when pasted into prompts.
	CaseSensitive bool
	// MaskDigits collapses every run of decimal digits inside a token to a
	// single "0", so templated contamination ("question 17 of 40" vs
	// "question 3 of 40") still lines up.
	MaskDigits bool
}

// Tokenize splits s into maximal runs of Unicode letters and digits.
// Punctuation, whitespace and symbols act as separators and never appear in
// tokens, which makes matching robust to formatting churn (smart quotes,
// re-wrapped lines, Markdown markers) between corpus and eval set.
func Tokenize(s string, opt Options) []Token {
	var out []Token
	start := -1
	for i, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			out = append(out, makeToken(s, start, i, opt))
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, makeToken(s, start, len(s), opt))
	}
	return out
}

// Texts projects a token slice to its normalized strings, the form the
// shingle hasher consumes.
func Texts(tokens []Token) []string {
	out := make([]string, len(tokens))
	for i, t := range tokens {
		out[i] = t.Text
	}
	return out
}

func makeToken(s string, start, end int, opt Options) Token {
	text := s[start:end]
	if !opt.CaseSensitive {
		text = strings.ToLower(text)
	}
	if opt.MaskDigits {
		text = maskDigits(text)
	}
	return Token{Text: text, Start: start, End: end}
}

// maskDigits replaces every maximal run of decimal digits with a single '0'.
// "q17of40" -> "q0of0"; tokens without digits are returned unchanged.
func maskDigits(s string) string {
	hasDigit := false
	for _, r := range s {
		if unicode.IsDigit(r) {
			hasDigit = true
			break
		}
	}
	if !hasDigit {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	inDigits := false
	for _, r := range s {
		if unicode.IsDigit(r) {
			if !inDigits {
				b.WriteByte('0')
				inDigits = true
			}
			continue
		}
		inDigits = false
		b.WriteRune(r)
	}
	return b.String()
}
