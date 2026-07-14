// Tests for the three renderers: header content, evidence lines, JSON
// schema stability and byte-level determinism.
package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/JaydenCJ/gramleak/internal/check"
	"github.com/JaydenCJ/gramleak/internal/corpus"
	"github.com/JaydenCJ/gramleak/internal/index"
)

// fixture builds a small deterministic summary: one flagged duplicate, one
// clean document, one skipped short document.
func fixture(t *testing.T) (check.Summary, Meta) {
	t.Helper()
	b := index.NewBuilder(index.Params{N: 3})
	b.AddText("the quick brown fox jumps over the lazy dog")
	ix := b.Build()
	c := check.New(ix, check.Options{ThresholdPct: 5})
	c.Add(corpus.Doc{ID: "eval/dup.txt", Text: "the quick brown fox jumps over the lazy dog"})
	c.Add(corpus.Doc{ID: "eval/clean.txt", Text: "completely unrelated words appear in this document"})
	c.Add(corpus.Doc{ID: "eval/tiny.txt", Text: "too short"})
	return c.Finish(), Meta{EvalLabel: "eval/", IndexLabel: "corpus.glx", Index: ix}
}

func TestTextHeaderNamesEvalAndIndex(t *testing.T) {
	sum, m := fixture(t)
	var buf bytes.Buffer
	Text(&buf, sum, m)
	out := buf.String()
	for _, want := range []string{
		"gramleak check — eval/ vs corpus.glx",
		"(n=3, case-folded, digits verbatim)",
		"flagged  1 of 2 documents at ≥ 5.0%",
		"1 skipped (too short)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("text output missing %q:\n%s", want, out)
		}
	}
}

func TestTextShowsGaugeAndEvidenceForFlagged(t *testing.T) {
	sum, m := fixture(t)
	var buf bytes.Buffer
	Text(&buf, sum, m)
	out := buf.String()
	if !strings.Contains(out, "█") || !strings.Contains(out, "░") {
		t.Fatalf("gauges missing:\n%s", out)
	}
	if !strings.Contains(out, "eval/dup.txt") || !strings.Contains(out, "└─ 9 tokens:") {
		t.Fatalf("evidence missing:\n%s", out)
	}
	// Clean documents are not listed unless ShowAll.
	if strings.Contains(out, "eval/clean.txt") {
		t.Fatalf("clean doc listed without --all:\n%s", out)
	}
}

func TestTextShowAllListsCleanDocs(t *testing.T) {
	sum, m := fixture(t)
	m.ShowAll = true
	var buf bytes.Buffer
	Text(&buf, sum, m)
	if !strings.Contains(buf.String(), "eval/clean.txt") {
		t.Fatalf("--all did not list clean doc:\n%s", buf.String())
	}
}

func TestRenderersAreDeterministic(t *testing.T) {
	sum, m := fixture(t)
	var a, b bytes.Buffer
	Text(&a, sum, m)
	Text(&b, sum, m)
	if a.String() != b.String() {
		t.Fatal("text renders differ between runs")
	}
	a.Reset()
	b.Reset()
	if err := JSON(&a, sum, m); err != nil {
		t.Fatal(err)
	}
	if err := JSON(&b, sum, m); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("JSON renders differ between runs")
	}
}

func TestJSONEnvelopeAndSchema(t *testing.T) {
	sum, m := fixture(t)
	var buf bytes.Buffer
	if err := JSON(&buf, sum, m); err != nil {
		t.Fatal(err)
	}
	var rep map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rep); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if rep["tool"] != "gramleak" || rep["schema_version"] != float64(1) {
		t.Fatalf("envelope %v", rep)
	}
	summary := rep["summary"].(map[string]any)
	if summary["documents"] != float64(2) || summary["skipped"] != float64(1) || summary["flagged"] != float64(1) {
		t.Fatalf("summary %v", summary)
	}
	if rep["index"].(map[string]any)["source"] != "corpus.glx" {
		t.Fatalf("index meta %v", rep["index"])
	}
}

func TestJSONDocumentsCarrySpansAndFlags(t *testing.T) {
	sum, m := fixture(t)
	var buf bytes.Buffer
	if err := JSON(&buf, sum, m); err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Documents []struct {
			ID      string  `json:"id"`
			Pct     float64 `json:"contamination_pct"`
			Flagged bool    `json:"flagged"`
			Spans   []struct {
				Tokens int    `json:"tokens"`
				Text   string `json:"text"`
			} `json:"spans"`
		} `json:"documents"`
	}
	if err := json.Unmarshal(buf.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Documents) != 2 {
		t.Fatalf("documents %v", rep.Documents)
	}
	top := rep.Documents[0]
	if top.ID != "eval/dup.txt" || !top.Flagged || top.Pct != 100 || len(top.Spans) != 1 {
		t.Fatalf("top doc %+v", top)
	}
}

func TestJSONCleanDocSpansAreEmptyArrayNotNull(t *testing.T) {
	sum, m := fixture(t)
	var buf bytes.Buffer
	if err := JSON(&buf, sum, m); err != nil {
		t.Fatal(err)
	}
	// jq users iterate .documents[].spans; null would break them.
	if strings.Contains(buf.String(), `"spans": null`) {
		t.Fatalf("null spans leaked:\n%s", buf.String())
	}
}

func TestMarkdownTableAndEvidence(t *testing.T) {
	sum, m := fixture(t)
	var buf bytes.Buffer
	Markdown(&buf, sum, m)
	out := buf.String()
	for _, want := range []string{
		"## gramleak report — eval/",
		"| Document | Contamination | Longest run | Matched shingles |",
		"| `eval/dup.txt` | 100.0% | 9 tokens | 7/7 |",
		"### Evidence (longest overlap per flagged document)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("markdown missing %q:\n%s", want, out)
		}
	}
}

func TestGaugeBoundaries(t *testing.T) {
	if g := gauge(0); strings.Contains(g, "█") {
		t.Fatalf("0%% gauge has filled cells: %q", g)
	}
	if g := gauge(100); strings.Contains(g, "░") {
		t.Fatalf("100%% gauge has empty cells: %q", g)
	}
	if g := gauge(150); len([]rune(g)) != gaugeWidth {
		t.Fatalf("gauge must clamp and stay fixed width: %q", g)
	}
}

func TestCommasFormatting(t *testing.T) {
	cases := map[int64]string{
		0:        "0",
		999:      "999",
		1000:     "1,000",
		48213:    "48,213",
		1234567:  "1,234,567",
		-1234567: "-1,234,567",
	}
	for in, want := range cases {
		if got := commas(in); got != want {
			t.Fatalf("commas(%d) = %q, want %q", in, got, want)
		}
	}
	if round2(33.333333) != 33.33 || round2(66.666666) != 66.67 {
		t.Fatal("round2 not rounding to two decimals")
	}
}
