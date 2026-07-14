// Package check runs eval documents against a shingle index and measures
// contamination: which fraction of each document's tokens is covered by
// n-grams that also occur in the corpus, with the matching spans
// reconstructed from byte offsets as quotable evidence.
package check

import (
	"sort"
	"strings"

	"github.com/JaydenCJ/gramleak/internal/corpus"
	"github.com/JaydenCJ/gramleak/internal/index"
	"github.com/JaydenCJ/gramleak/internal/shingle"
	"github.com/JaydenCJ/gramleak/internal/tokenize"
)

// Options tune flagging and evidence collection.
type Options struct {
	// ThresholdPct flags a document when its token coverage reaches this
	// percentage. The CLI defaults it to 5.0; zero flags every document
	// with any overlap at all.
	ThresholdPct float64
	// MinTokens skips documents shorter than this many tokens; it is
	// raised to the shingle size n if smaller, since sub-window documents
	// produce no shingles at all.
	MinTokens int
	// TopSpans caps the evidence spans kept per document (longest first).
	// Default 3.
	TopSpans int
	// MaxSpanChars truncates evidence text for display. Default 160.
	MaxSpanChars int
}

// Normalize fills in defaults for zero-valued fields.
func (o Options) Normalize(n int) Options {
	if o.TopSpans == 0 {
		o.TopSpans = 3
	}
	if o.MaxSpanChars == 0 {
		o.MaxSpanChars = 160
	}
	if o.MinTokens < n {
		o.MinTokens = n
	}
	return o
}

// Span is one contiguous run of matched shingles mapped back to the
// original document text.
type Span struct {
	StartByte int    `json:"start_byte"`
	EndByte   int    `json:"end_byte"`
	Tokens    int    `json:"tokens"`
	Text      string `json:"text"`
}

// DocResult is the contamination measurement for one eval document.
type DocResult struct {
	ID              string  `json:"id"`
	Tokens          int     `json:"tokens"`
	Shingles        int     `json:"shingles"`
	MatchedShingles int     `json:"matched_shingles"`
	CoveredTokens   int     `json:"covered_tokens"`
	Pct             float64 `json:"contamination_pct"` // covered/total tokens ×100
	LongestRun      int     `json:"longest_run_tokens"`
	Flagged         bool    `json:"flagged"`
	Spans           []Span  `json:"spans,omitempty"`
}

// Summary aggregates a whole check run. Results are sorted by
// contamination descending, then ID, so reports are deterministic.
type Summary struct {
	Docs          int
	Skipped       int
	Flagged       int
	ThresholdPct  float64
	TotalTokens   int64
	CoveredTokens int64
	OverallPct    float64
	MaxPct        float64
	MaxDoc        string
	Results       []DocResult
}

// Checker streams eval documents against one index.
type Checker struct {
	ix      *index.Index
	opt     Options
	results []DocResult
	skipped int
	total   int64
	covered int64
}

// New returns a Checker for ix with normalized options.
func New(ix *index.Index, opt Options) *Checker {
	return &Checker{ix: ix, opt: opt.Normalize(ix.Params.N)}
}

// Add measures one document. Documents shorter than MinTokens are counted
// as skipped, not silently dropped.
func (c *Checker) Add(doc corpus.Doc) {
	toks := c.ix.Params.Tokenize(doc.Text)
	if len(toks) < c.opt.MinTokens {
		c.skipped++
		return
	}
	res := measure(doc, toks, c.ix, c.opt)
	c.total += int64(res.Tokens)
	c.covered += int64(res.CoveredTokens)
	c.results = append(c.results, res)
}

// Finish sorts results and computes the aggregate summary.
func (c *Checker) Finish() Summary {
	sort.Slice(c.results, func(i, j int) bool {
		a, b := c.results[i], c.results[j]
		if a.Pct != b.Pct {
			return a.Pct > b.Pct
		}
		return a.ID < b.ID
	})
	sum := Summary{
		Docs:          len(c.results),
		Skipped:       c.skipped,
		ThresholdPct:  c.opt.ThresholdPct,
		TotalTokens:   c.total,
		CoveredTokens: c.covered,
		Results:       c.results,
	}
	if c.total > 0 {
		sum.OverallPct = 100 * float64(c.covered) / float64(c.total)
	}
	for _, r := range c.results {
		if r.Flagged {
			sum.Flagged++
		}
		if r.Pct > sum.MaxPct || (r.Pct == sum.MaxPct && sum.MaxDoc == "") {
			sum.MaxPct = r.Pct
			sum.MaxDoc = r.ID
		}
	}
	return sum
}

// measure computes coverage for one document. Every matched shingle at
// window position i marks tokens i..i+n-1 as covered; contiguous matched
// windows merge into a single evidence span.
func measure(doc corpus.Doc, toks []tokenize.Token, ix *index.Index, opt Options) DocResult {
	n := ix.Params.N
	hashes := shingle.Hashes(tokenize.Texts(toks), n)
	covered := make([]bool, len(toks))
	matched := 0
	var spans []Span
	runStart := -1 // first window index of the current matched run
	runEnd := -1   // last window index of the current matched run
	flush := func() {
		if runStart < 0 {
			return
		}
		first, last := runStart, runEnd+n-1 // token range of the run
		spans = append(spans, Span{
			StartByte: toks[first].Start,
			EndByte:   toks[last].End,
			Tokens:    last - first + 1,
			Text:      excerpt(doc.Text[toks[first].Start:toks[last].End], opt.MaxSpanChars),
		})
		runStart, runEnd = -1, -1
	}
	for i, h := range hashes {
		if !ix.Contains(h) {
			flush()
			continue
		}
		matched++
		for j := i; j < i+n; j++ {
			covered[j] = true
		}
		if runStart < 0 {
			runStart = i
		}
		runEnd = i
	}
	flush()

	coveredCount := 0
	for _, c := range covered {
		if c {
			coveredCount++
		}
	}
	longest := 0
	for _, s := range spans {
		if s.Tokens > longest {
			longest = s.Tokens
		}
	}
	// Longest spans first; ties keep document order (earlier byte first).
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].Tokens > spans[j].Tokens })
	if len(spans) > opt.TopSpans {
		spans = spans[:opt.TopSpans]
	}
	pct := 100 * float64(coveredCount) / float64(len(toks))
	return DocResult{
		ID:              doc.ID,
		Tokens:          len(toks),
		Shingles:        len(hashes),
		MatchedShingles: matched,
		CoveredTokens:   coveredCount,
		Pct:             pct,
		LongestRun:      longest,
		Flagged:         coveredCount > 0 && pct >= opt.ThresholdPct,
		Spans:           spans,
	}
}

// excerpt collapses whitespace for single-line display and truncates to
// max characters on a rune boundary with an ellipsis.
func excerpt(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
