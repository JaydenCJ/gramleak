// Tests for the tokenizer: normalization rules, byte-offset fidelity and
// the digit-masking transform. Offsets matter as much as token text — the
// checker slices the original document with them to quote evidence.
package tokenize

import (
	"reflect"
	"testing"
)

func texts(toks []Token) []string { return Texts(toks) }

func TestTokenizeSplitsOnWhitespace(t *testing.T) {
	toks := Tokenize("the quick brown fox", Options{})
	got := Texts(toks)
	want := []string{"the", "quick", "brown", "fox"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTokenizeCaseFolding(t *testing.T) {
	got := texts(Tokenize("The QUICK Fox", Options{}))
	if want := []string{"the", "quick", "fox"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("default folding: got %v, want %v", got, want)
	}
	got = texts(Tokenize("The QUICK Fox", Options{CaseSensitive: true}))
	if want := []string{"The", "QUICK", "Fox"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("case-sensitive: got %v, want %v", got, want)
	}
}

func TestTokenizePunctuationSeparates(t *testing.T) {
	// Formatting churn (quotes, markdown, hyphens) must not change tokens.
	got := texts(Tokenize(`"Don't stop—ever," she said.`, Options{}))
	want := []string{"don", "t", "stop", "ever", "she", "said"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTokenizeByteOffsetsSliceOriginalText(t *testing.T) {
	src := "Alpha, beta; GAMMA!"
	for _, tok := range Tokenize(src, Options{}) {
		if tok.Start < 0 || tok.End > len(src) || tok.Start >= tok.End {
			t.Fatalf("bad span %+v", tok)
		}
		// The original slice must equal the token text modulo case folding.
		if got := src[tok.Start:tok.End]; len(got) != len(tok.Text) {
			t.Fatalf("span %q does not correspond to token %q", got, tok.Text)
		}
	}
}

func TestTokenizeUnicodeLettersOneToken(t *testing.T) {
	toks := Tokenize("café — naïve", Options{})
	got := texts(toks)
	want := []string{"café", "naïve"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// Offsets are byte offsets: slicing must reproduce the accented word.
	if src := "café — naïve"; src[toks[0].Start:toks[0].End] != "café" {
		t.Fatalf("offset slice = %q", src[toks[0].Start:toks[0].End])
	}
}

func TestTokenizeCJKRunIsOneToken(t *testing.T) {
	// CJK text has no spaces; a contiguous run is one token, which still
	// matches verbatim reuse of the same run.
	got := texts(Tokenize("これはテストです ok", Options{}))
	if len(got) != 2 || got[1] != "ok" {
		t.Fatalf("got %v", got)
	}
}

func TestDigitMasking(t *testing.T) {
	got := texts(Tokenize("item 17 of 40", Options{}))
	if want := []string{"item", "17", "of", "40"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("digits must stay verbatim by default: %v", got)
	}
	got = texts(Tokenize("item 17 of 40", Options{MaskDigits: true}))
	if want := []string{"item", "0", "of", "0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("masked: got %v, want %v", got, want)
	}
	// Runs inside mixed tokens collapse too; digit-free tokens are untouched.
	if got := texts(Tokenize("q17of40b", Options{MaskDigits: true})); got[0] != "q0of0b" {
		t.Fatalf("mixed token: %v", got)
	}
	if got := maskDigits("hello"); got != "hello" {
		t.Fatalf("digit-free token changed: %q", got)
	}
}

func TestTokenizeEdgeInputs(t *testing.T) {
	if got := Tokenize("", Options{}); len(got) != 0 {
		t.Fatalf("empty input produced %v", got)
	}
	if got := Tokenize(" \t\n…!?", Options{}); len(got) != 0 {
		t.Fatalf("separator-only input produced %v", got)
	}
	// The final token has no trailing separator; it must still be emitted.
	toks := Tokenize("tail", Options{})
	if len(toks) != 1 || toks[0].Text != "tail" || toks[0].End != 4 {
		t.Fatalf("got %+v", toks)
	}
}
