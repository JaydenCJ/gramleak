// Package render turns a check summary into terminal text, stable JSON
// (schema_version 1) or PR-ready Markdown. All three are deterministic:
// identical input produces byte-identical output.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/JaydenCJ/gramleak/internal/check"
	"github.com/JaydenCJ/gramleak/internal/index"
	"github.com/JaydenCJ/gramleak/internal/version"
)

// Meta describes where the eval set and index came from, for report headers.
type Meta struct {
	EvalLabel  string // e.g. "eval/"
	IndexLabel string // e.g. "corpus.glx" or "in-memory (data/train.jsonl)"
	Index      *index.Index
	ShowAll    bool // text/markdown: list clean documents too
}

const gaugeWidth = 24

// Text writes the human report.
func Text(w io.Writer, sum check.Summary, m Meta) {
	fmt.Fprintf(w, "gramleak check — %s vs %s\n", m.EvalLabel, m.IndexLabel)
	fmt.Fprintf(w, "index: %s shingle%s (%s) from %s document%s / %s token%s\n\n",
		commas(int64(m.Index.Len())), plural(m.Index.Len()), m.Index.Params.Describe(),
		commas(int64(m.Index.Docs)), plural(m.Index.Docs), commas(m.Index.Tokens), plural(m.Index.Tokens))

	fmt.Fprintf(w, "contamination\n")
	fmt.Fprintf(w, "  overall  %s  %5.1f%%  (%s/%s token%s)\n",
		gauge(sum.OverallPct), sum.OverallPct, commas(sum.CoveredTokens), commas(sum.TotalTokens), plural(sum.TotalTokens))
	if sum.MaxDoc != "" {
		fmt.Fprintf(w, "  worst    %s at %.1f%%\n", sum.MaxDoc, sum.MaxPct)
	}
	fmt.Fprintf(w, "  flagged  %d of %d document%s at ≥ %.1f%%\n",
		sum.Flagged, sum.Docs, plural(sum.Docs), sum.ThresholdPct)

	listed := listed(sum, m.ShowAll)
	if len(listed) > 0 {
		if m.ShowAll {
			fmt.Fprintf(w, "\nall documents\n")
		} else {
			fmt.Fprintf(w, "\nflagged documents\n")
		}
		for _, r := range listed {
			fmt.Fprintf(w, "  %5.1f%%  %s  %s  (longest run %d token%s)\n",
				r.Pct, gauge(r.Pct), r.ID, r.LongestRun, plural(r.LongestRun))
			for _, s := range r.Spans {
				fmt.Fprintf(w, "          └─ %d token%s: %q\n", s.Tokens, plural(s.Tokens), s.Text)
			}
		}
	}

	fmt.Fprintf(w, "\n%d document%s checked", sum.Docs, plural(sum.Docs))
	if sum.Skipped > 0 {
		fmt.Fprintf(w, ", %d skipped (too short)", sum.Skipped)
	}
	fmt.Fprintln(w)
}

// jsonReport is the schema_version 1 envelope. Field order is fixed by
// this struct; downstream tooling can rely on it.
type jsonReport struct {
	Tool          string      `json:"tool"`
	Version       string      `json:"version"`
	SchemaVersion int         `json:"schema_version"`
	Params        jsonParams  `json:"params"`
	Index         jsonIndex   `json:"index"`
	Summary       jsonSummary `json:"summary"`
	Documents     []jsonDoc   `json:"documents"`
}

type jsonParams struct {
	N             int  `json:"n"`
	CaseSensitive bool `json:"case_sensitive"`
	MaskDigits    bool `json:"mask_digits"`
}

type jsonIndex struct {
	Source    string `json:"source"`
	Shingles  int    `json:"shingles"`
	Documents int    `json:"documents"`
	Tokens    int64  `json:"tokens"`
}

type jsonSummary struct {
	Documents     int     `json:"documents"`
	Skipped       int     `json:"skipped"`
	Flagged       int     `json:"flagged"`
	ThresholdPct  float64 `json:"threshold_pct"`
	TotalTokens   int64   `json:"total_tokens"`
	CoveredTokens int64   `json:"covered_tokens"`
	OverallPct    float64 `json:"overall_pct"`
	MaxPct        float64 `json:"max_pct"`
	MaxDocument   string  `json:"max_document"`
}

type jsonDoc struct {
	ID              string       `json:"id"`
	Tokens          int          `json:"tokens"`
	Shingles        int          `json:"shingles"`
	MatchedShingles int          `json:"matched_shingles"`
	CoveredTokens   int          `json:"covered_tokens"`
	Pct             float64      `json:"contamination_pct"`
	LongestRun      int          `json:"longest_run_tokens"`
	Flagged         bool         `json:"flagged"`
	Spans           []check.Span `json:"spans"`
}

// JSON writes the machine report. Percentages are rounded to two decimals
// so re-runs on identical data are byte-identical across platforms.
func JSON(w io.Writer, sum check.Summary, m Meta) error {
	rep := jsonReport{
		Tool:          "gramleak",
		Version:       version.Version,
		SchemaVersion: 1,
		Params: jsonParams{
			N:             m.Index.Params.N,
			CaseSensitive: m.Index.Params.CaseSensitive,
			MaskDigits:    m.Index.Params.MaskDigits,
		},
		Index: jsonIndex{
			Source:    m.IndexLabel,
			Shingles:  m.Index.Len(),
			Documents: m.Index.Docs,
			Tokens:    m.Index.Tokens,
		},
		Summary: jsonSummary{
			Documents:     sum.Docs,
			Skipped:       sum.Skipped,
			Flagged:       sum.Flagged,
			ThresholdPct:  round2(sum.ThresholdPct),
			TotalTokens:   sum.TotalTokens,
			CoveredTokens: sum.CoveredTokens,
			OverallPct:    round2(sum.OverallPct),
			MaxPct:        round2(sum.MaxPct),
			MaxDocument:   sum.MaxDoc,
		},
		Documents: make([]jsonDoc, 0, len(sum.Results)),
	}
	for _, r := range sum.Results {
		spans := r.Spans
		if spans == nil {
			spans = []check.Span{}
		}
		rep.Documents = append(rep.Documents, jsonDoc{
			ID:              r.ID,
			Tokens:          r.Tokens,
			Shingles:        r.Shingles,
			MatchedShingles: r.MatchedShingles,
			CoveredTokens:   r.CoveredTokens,
			Pct:             round2(r.Pct),
			LongestRun:      r.LongestRun,
			Flagged:         r.Flagged,
			Spans:           spans,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}

// Markdown writes a PR-comment-ready report.
func Markdown(w io.Writer, sum check.Summary, m Meta) {
	fmt.Fprintf(w, "## gramleak report — %s\n\n", m.EvalLabel)
	fmt.Fprintf(w, "**Overall contamination: %.1f%%** (%s / %s token%s) — %d of %d document%s at ≥ %.1f%%. Index: %s shingle%s (%s).\n\n",
		sum.OverallPct, commas(sum.CoveredTokens), commas(sum.TotalTokens), plural(sum.TotalTokens),
		sum.Flagged, sum.Docs, plural(sum.Docs), sum.ThresholdPct,
		commas(int64(m.Index.Len())), plural(m.Index.Len()), m.Index.Params.Describe())

	listedDocs := listed(sum, m.ShowAll)
	if len(listedDocs) > 0 {
		fmt.Fprintf(w, "| Document | Contamination | Longest run | Matched shingles |\n")
		fmt.Fprintf(w, "|---|---:|---:|---:|\n")
		for _, r := range listedDocs {
			fmt.Fprintf(w, "| `%s` | %.1f%% | %d token%s | %d/%d |\n",
				r.ID, r.Pct, r.LongestRun, plural(r.LongestRun), r.MatchedShingles, r.Shingles)
		}
		var evid []string
		for _, r := range listedDocs {
			if !r.Flagged || len(r.Spans) == 0 {
				continue
			}
			evid = append(evid, fmt.Sprintf("- `%s` — %d token%s: “%s”", r.ID, r.Spans[0].Tokens, plural(r.Spans[0].Tokens), r.Spans[0].Text))
		}
		if len(evid) > 0 {
			fmt.Fprintf(w, "\n### Evidence (longest overlap per flagged document)\n\n%s\n", strings.Join(evid, "\n"))
		}
	}
}

func listed(sum check.Summary, all bool) []check.DocResult {
	if all {
		return sum.Results
	}
	var out []check.DocResult
	for _, r := range sum.Results {
		if r.Flagged {
			out = append(out, r)
		}
	}
	return out
}

// gauge renders pct as a fixed-width block bar, clamped to [0,100].
func gauge(pct float64) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(math.Round(pct / 100 * gaugeWidth))
	return strings.Repeat("█", filled) + strings.Repeat("░", gaugeWidth-filled)
}

// commas formats n with thousands separators ("48213" -> "48,213").
func commas(n int64) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	out := strings.Join(parts, ",")
	if neg {
		out = "-" + out
	}
	return out
}

func round2(x float64) float64 { return math.Round(x*100) / 100 }

// plural returns "s" unless n is exactly 1, so reports never print
// "1 documents" or "1 tokens".
func plural[T ~int | ~int64](n T) string {
	if n == 1 {
		return ""
	}
	return "s"
}
