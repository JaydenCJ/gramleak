// Tests for the contamination engine: coverage math, run/span
// reconstruction, threshold flagging and summary aggregation. Cases are
// built from tiny hand-computable corpora so every expected number is
// verifiable on paper.
package check

import (
	"strings"
	"testing"

	"github.com/JaydenCJ/gramleak/internal/corpus"
	"github.com/JaydenCJ/gramleak/internal/index"
)

// ixOf builds an in-memory index over the given corpus texts.
func ixOf(t *testing.T, n int, texts ...string) *index.Index {
	t.Helper()
	b := index.NewBuilder(index.Params{N: n})
	for _, s := range texts {
		b.AddText(s)
	}
	return b.Build()
}

// one runs a single document through a fresh checker and returns its result.
func one(t *testing.T, ix *index.Index, opt Options, text string) DocResult {
	t.Helper()
	c := New(ix, opt)
	c.Add(corpus.Doc{ID: "doc", Text: text})
	sum := c.Finish()
	if len(sum.Results) != 1 {
		t.Fatalf("want 1 result, got %d (skipped=%d)", len(sum.Results), sum.Skipped)
	}
	return sum.Results[0]
}

func TestExactDuplicateIsFullyContaminated(t *testing.T) {
	src := "the quick brown fox jumps over the lazy dog"
	r := one(t, ixOf(t, 3, src), Options{}, src)
	if r.Pct != 100 || r.CoveredTokens != r.Tokens {
		t.Fatalf("duplicate doc: %+v", r)
	}
	if r.MatchedShingles != r.Shingles {
		t.Fatalf("all shingles should match: %+v", r)
	}
}

func TestDisjointDocumentIsClean(t *testing.T) {
	r := one(t, ixOf(t, 3, "alpha beta gamma delta epsilon"), Options{},
		"one two three four five six")
	if r.Pct != 0 || r.Flagged || len(r.Spans) != 0 || r.LongestRun != 0 {
		t.Fatalf("clean doc misreported: %+v", r)
	}
}

func TestPartialOverlapCoverageMath(t *testing.T) {
	// Corpus holds "a b c". Eval "a b c x y" with n=3 has windows
	// [a b c][b c x][c x y]; only the first matches, covering tokens 0-2.
	// Expected: 3 of 5 tokens = 60%.
	r := one(t, ixOf(t, 3, "a b c"), Options{}, "a b c x y")
	if r.CoveredTokens != 3 || r.Tokens != 5 {
		t.Fatalf("coverage: %+v", r)
	}
	if r.Pct != 60 {
		t.Fatalf("pct = %v, want 60", r.Pct)
	}
	if r.MatchedShingles != 1 || r.Shingles != 3 {
		t.Fatalf("shingles: %+v", r)
	}
}

func TestCaseAndPunctuationChurnStillMatches(t *testing.T) {
	ix := ixOf(t, 4, "What is the capital of France? Paris.")
	r := one(t, ix, Options{}, "WHAT is the CAPITAL — of france: paris")
	if r.Pct != 100 {
		t.Fatalf("re-formatted duplicate not caught: %+v", r)
	}
}

func TestShortDocumentsAreSkippedNotZero(t *testing.T) {
	// Sub-window documents cannot produce shingles; reporting them as 0%
	// clean would be misleading, so they are counted as skipped instead.
	c := New(ixOf(t, 5, "a b c d e f"), Options{})
	c.Add(corpus.Doc{ID: "tiny", Text: "too short"})
	sum := c.Finish()
	if sum.Docs != 0 || sum.Skipped != 1 {
		t.Fatalf("summary %+v", sum)
	}
	// --min-tokens raises the bar beyond n.
	c = New(ixOf(t, 2, "a b c"), Options{MinTokens: 10})
	c.Add(corpus.Doc{ID: "short", Text: "one two three four five"})
	if sum := c.Finish(); sum.Skipped != 1 {
		t.Fatalf("min-tokens not applied: %+v", sum)
	}
}

func TestThresholdComparisonIsInclusive(t *testing.T) {
	// 60% contamination, threshold 60 → flagged; threshold 60.1 → not.
	r := one(t, ixOf(t, 3, "a b c"), Options{ThresholdPct: 60}, "a b c x y")
	if !r.Flagged {
		t.Fatalf("threshold should be inclusive: %+v", r)
	}
	r = one(t, ixOf(t, 3, "a b c"), Options{ThresholdPct: 60.1}, "a b c x y")
	if r.Flagged {
		t.Fatalf("60%% must not flag at 60.1 threshold: %+v", r)
	}
}

func TestZeroThresholdFlagsAnyOverlapButNotCleanDocs(t *testing.T) {
	ix := ixOf(t, 3, "a b c")
	if r := one(t, ix, Options{ThresholdPct: 0}, "a b c x y"); !r.Flagged {
		t.Fatal("any overlap should flag at threshold 0")
	}
	if r := one(t, ix, Options{ThresholdPct: 0}, "p q r s t"); r.Flagged {
		t.Fatal("clean doc flagged at threshold 0")
	}
}

func TestLongestRunSpansContiguousWindows(t *testing.T) {
	// Corpus contains a 6-token phrase; eval embeds it verbatim. With n=3
	// the matched windows are contiguous and the merged run is 6 tokens.
	ix := ixOf(t, 3, "one two three four five six")
	r := one(t, ix, Options{}, "zz one two three four five six yy ww qq")
	if r.LongestRun != 6 {
		t.Fatalf("longest run = %d, want 6 (%+v)", r.LongestRun, r)
	}
	if len(r.Spans) != 1 || r.Spans[0].Tokens != 6 {
		t.Fatalf("spans %+v", r.Spans)
	}
}

func TestSpanTextQuotesOriginalBytes(t *testing.T) {
	ix := ixOf(t, 3, "the answer is forty two")
	src := "Preamble!! The answer, IS forty-two; trailing words here."
	r := one(t, ix, Options{}, src)
	if len(r.Spans) == 0 {
		t.Fatal("expected a span")
	}
	s := r.Spans[0]
	// The span must slice the ORIGINAL text (original case/punctuation),
	// whitespace-collapsed for display.
	if s.Text != "The answer, IS forty-two" {
		t.Fatalf("span text %q", s.Text)
	}
	if src[s.StartByte:s.StartByte+3] != "The" {
		t.Fatalf("start byte wrong: %d", s.StartByte)
	}
}

func TestSeparateRunsAndTopSpansLimit(t *testing.T) {
	// Two non-adjacent matches become two spans...
	ix := ixOf(t, 2, "aa bb", "cc dd")
	r := one(t, ix, Options{}, "aa bb xx yy cc dd")
	if len(r.Spans) != 2 || r.Spans[0].Tokens != 2 || r.Spans[1].Tokens != 2 {
		t.Fatalf("want 2 two-token spans, got %+v", r.Spans)
	}
	// ...and TopSpans keeps only the longest.
	ix = ixOf(t, 2, "aa bb", "cc dd ee ff")
	r = one(t, ix, Options{TopSpans: 1}, "aa bb xx cc dd ee ff yy zz")
	if len(r.Spans) != 1 || r.Spans[0].Tokens != 4 {
		t.Fatalf("want single longest span (4 tokens), got %+v", r.Spans)
	}
}

func TestSpanTextDisplayNormalization(t *testing.T) {
	// Long spans truncate on a rune boundary with an ellipsis.
	phrase := "aaaa bbbb cccc dddd eeee ffff gggg hhhh"
	r := one(t, ixOf(t, 3, phrase), Options{MaxSpanChars: 12}, phrase)
	got := r.Spans[0].Text
	if len([]rune(got)) > 12 || !strings.HasSuffix(got, "…") {
		t.Fatalf("truncation wrong: %q", got)
	}
	// Internal whitespace collapses to single spaces for one-line display.
	r = one(t, ixOf(t, 3, "line one and line two"), Options{}, "line one\n\tand   line two")
	if r.Spans[0].Text != "line one and line two" {
		t.Fatalf("got %q", r.Spans[0].Text)
	}
}

func TestResultsSortedByContaminationThenID(t *testing.T) {
	ix := ixOf(t, 2, "aa bb cc dd ee")
	c := New(ix, Options{})
	c.Add(corpus.Doc{ID: "clean-b", Text: "pp qq rr ss"})
	c.Add(corpus.Doc{ID: "dirty", Text: "aa bb cc dd ee"})
	c.Add(corpus.Doc{ID: "clean-a", Text: "tt uu vv ww"})
	sum := c.Finish()
	got := []string{sum.Results[0].ID, sum.Results[1].ID, sum.Results[2].ID}
	want := []string{"dirty", "clean-a", "clean-b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order %v, want %v", got, want)
		}
	}
}

func TestSummaryAggregatesTokensAndMax(t *testing.T) {
	ix := ixOf(t, 3, "a b c")
	c := New(ix, Options{ThresholdPct: 50})
	c.Add(corpus.Doc{ID: "hot", Text: "a b c x y"})  // 3/5 covered
	c.Add(corpus.Doc{ID: "cold", Text: "p q r s t"}) // 0/5
	sum := c.Finish()
	if sum.TotalTokens != 10 || sum.CoveredTokens != 3 {
		t.Fatalf("totals %+v", sum)
	}
	if sum.OverallPct != 30 {
		t.Fatalf("overall = %v, want 30", sum.OverallPct)
	}
	if sum.MaxDoc != "hot" || sum.MaxPct != 60 || sum.Flagged != 1 {
		t.Fatalf("max/flagged %+v", sum)
	}
}

func TestMaskedDigitsCatchTemplatedContamination(t *testing.T) {
	b := index.NewBuilder(index.Params{N: 4, MaskDigits: true})
	b.AddText("question 17 of 40 answer below")
	ix := b.Build()
	r := one(t, ix, Options{}, "question 3 of 40 answer below")
	if r.Pct != 100 {
		t.Fatalf("digit-masked match failed: %+v", r)
	}
}
