// In-process integration tests for the CLI: real files in temp dirs, real
// subcommand flows, and the exit-code contract scripts depend on.
package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaydenCJ/gramleak/internal/version"
)

// run executes the CLI in-process and captures stdio.
func run(args ...string) (code int, stdout, stderr string) {
	var out, errBuf bytes.Buffer
	code = Run(args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

// write creates a file under dir, making parents as needed.
func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// fixture lays out a corpus with a known phrase and an eval set where one
// document reuses that phrase verbatim and one is clean.
func fixture(t *testing.T) (corpusDir, evalDir string) {
	t.Helper()
	root := t.TempDir()
	corpusDir = filepath.Join(root, "corpus")
	evalDir = filepath.Join(root, "eval")
	write(t, corpusDir, "train.txt",
		"The mitochondria is the powerhouse of the cell and produces energy for the organism.\n"+
			"Photosynthesis converts light energy into chemical energy inside chloroplasts every day.\n")
	write(t, evalDir, "leaked.txt",
		"Q: The mitochondria is the powerhouse of the cell and produces energy for the organism?\n")
	write(t, evalDir, "clean.txt",
		"Q: Which chess opening sacrifices a pawn for rapid piece development in exchange?\n")
	return corpusDir, evalDir
}

// buildIndexFile runs `gramleak index` and returns the .glx path.
func buildIndexFile(t *testing.T, corpusDir string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "corpus.glx")
	code, stdout, stderr := run("index", "--out", out, corpusDir)
	if code != ExitOK {
		t.Fatalf("index failed (%d): %s%s", code, stdout, stderr)
	}
	return out
}

func TestVersionCommandAndAlias(t *testing.T) {
	code, out, _ := run("version")
	if code != ExitOK || strings.TrimSpace(out) != "gramleak "+version.Version {
		t.Fatalf("code=%d out=%q", code, out)
	}
	code, out, _ = run("--version")
	if code != ExitOK || !strings.Contains(out, version.Version) {
		t.Fatalf("alias: code=%d out=%q", code, out)
	}
}

func TestHelpExitsZero(t *testing.T) {
	code, out, _ := run("help")
	if code != ExitOK || !strings.Contains(out, "gramleak index") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestBadInvocationsExit2(t *testing.T) {
	code, _, errOut := run()
	if code != ExitUsage || !strings.Contains(errOut, "Usage:") {
		t.Fatalf("no args: code=%d err=%q", code, errOut)
	}
	code, _, errOut = run("frobnicate")
	if code != ExitUsage || !strings.Contains(errOut, "unknown command") {
		t.Fatalf("unknown command: code=%d err=%q", code, errOut)
	}
}

func TestIndexRequiresOutAndCorpus(t *testing.T) {
	corpusDir, _ := fixture(t)
	code, _, errOut := run("index", corpusDir)
	if code != ExitUsage || !strings.Contains(errOut, "--out") {
		t.Fatalf("missing --out: code=%d err=%q", code, errOut)
	}
	code, _, _ = run("index", "--out", filepath.Join(t.TempDir(), "x.glx"))
	if code != ExitUsage {
		t.Fatalf("missing corpus path: code=%d", code)
	}
}

func TestIndexReportsCountsAndWritesFile(t *testing.T) {
	corpusDir, _ := fixture(t)
	out := filepath.Join(t.TempDir(), "c.glx")
	code, stdout, _ := run("index", "--out", out, corpusDir)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(stdout, "indexed 1 document /") || !strings.Contains(stdout, "wrote "+out) {
		t.Fatalf("stdout %q", stdout)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
}

func TestIndexEmptyCorpusExits3(t *testing.T) {
	code, _, errOut := run("index", "--out", filepath.Join(t.TempDir(), "x.glx"), t.TempDir())
	if code != ExitRuntime || !strings.Contains(errOut, "no corpus documents") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestStatsDescribesIndex(t *testing.T) {
	corpusDir, _ := fixture(t)
	glx := buildIndexFile(t, corpusDir)
	code, out, _ := run("stats", glx)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	for _, want := range []string{"GLXI v1", "n          8", "case-folded", "1 document /"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stats missing %q:\n%s", want, out)
		}
	}
}

func TestCorruptIndexExits3Everywhere(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "junk.glx", "this is definitely not an index file, but it is long enough to read")
	code, _, errOut := run("stats", p)
	if code != ExitRuntime || !strings.Contains(errOut, "not a gramleak index") {
		t.Fatalf("stats: code=%d err=%q", code, errOut)
	}
	_, evalDir := fixture(t)
	code, _, errOut = run("check", "--index", p, evalDir)
	if code != ExitRuntime || errOut == "" {
		t.Fatalf("check: code=%d err=%q", code, errOut)
	}
}

func TestCheckEndToEndWithIndexFile(t *testing.T) {
	corpusDir, evalDir := fixture(t)
	glx := buildIndexFile(t, corpusDir)
	code, out, _ := run("check", "--index", glx, evalDir)
	if code != ExitOK {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "leaked.txt") {
		t.Fatalf("leaked doc not reported:\n%s", out)
	}
	if strings.Contains(out, "clean.txt") {
		t.Fatalf("clean doc listed without --all:\n%s", out)
	}
	if !strings.Contains(out, "flagged  1 of 2 documents") {
		t.Fatalf("flag summary wrong:\n%s", out)
	}
}

func TestCheckAgainstBuildsInMemoryIndex(t *testing.T) {
	corpusDir, evalDir := fixture(t)
	code, out, _ := run("check", "--against", corpusDir, evalDir)
	if code != ExitOK || !strings.Contains(out, "in-memory ("+corpusDir+")") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "leaked.txt") {
		t.Fatalf("leaked doc not flagged:\n%s", out)
	}
}

func TestCheckSourceFlagValidation(t *testing.T) {
	corpusDir, evalDir := fixture(t)
	code, _, errOut := run("check", evalDir)
	if code != ExitUsage || !strings.Contains(errOut, "exactly one of") {
		t.Fatalf("neither source: code=%d err=%q", code, errOut)
	}
	code, _, _ = run("check", "--index", "x.glx", "--against", "y", evalDir)
	if code != ExitUsage {
		t.Fatalf("both sources: code=%d", code)
	}
	// Shingling params come from the index file; overrides must be refused.
	glx := buildIndexFile(t, corpusDir)
	code, _, errOut = run("check", "--index", glx, "-n", "5", evalDir)
	if code != ExitUsage || !strings.Contains(errOut, "conflicts with --index") {
		t.Fatalf("param conflict: code=%d err=%q", code, errOut)
	}
}

func TestCheckRejectsUnknownFormat(t *testing.T) {
	corpusDir, evalDir := fixture(t)
	glx := buildIndexFile(t, corpusDir)
	code, _, errOut := run("check", "--index", glx, "--format", "yaml", evalDir)
	if code != ExitUsage || !strings.Contains(errOut, "yaml") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestCheckFailOverGate(t *testing.T) {
	corpusDir, evalDir := fixture(t)
	glx := buildIndexFile(t, corpusDir)
	code, out, _ := run("check", "--index", glx, "--fail-over", "50", evalDir)
	if code != ExitGate || !strings.Contains(out, "→ FAIL") {
		t.Fatalf("breach: code=%d out=%q", code, out)
	}
	code, out, _ = run("check", "--index", glx, "--fail-over", "99.5", evalDir)
	if code != ExitOK || !strings.Contains(out, "→ ok") {
		t.Fatalf("pass: code=%d out=%q", code, out)
	}
}

func TestCheckJSONGateVerdictGoesToStderr(t *testing.T) {
	corpusDir, evalDir := fixture(t)
	glx := buildIndexFile(t, corpusDir)
	code, out, errOut := run("check", "--index", glx, "--format", "json", "--fail-over", "50", evalDir)
	if code != ExitGate {
		t.Fatalf("code=%d", code)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("stdout is not pure JSON:\n%s", out)
	}
	if !strings.Contains(errOut, "→ FAIL") {
		t.Fatalf("gate verdict not on stderr: %q", errOut)
	}
}

func TestCheckJSONOutputParsesWithExpectedNumbers(t *testing.T) {
	corpusDir, evalDir := fixture(t)
	glx := buildIndexFile(t, corpusDir)
	code, out, _ := run("check", "--index", glx, "--format", "json", evalDir)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	var rep struct {
		Tool    string `json:"tool"`
		Summary struct {
			Documents int     `json:"documents"`
			Flagged   int     `json:"flagged"`
			MaxPct    float64 `json:"max_pct"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Tool != "gramleak" || rep.Summary.Documents != 2 || rep.Summary.Flagged != 1 {
		t.Fatalf("report %+v", rep)
	}
	if rep.Summary.MaxPct < 80 {
		t.Fatalf("leaked doc should be heavily contaminated, got %v", rep.Summary.MaxPct)
	}
}

func TestCheckMarkdownOutput(t *testing.T) {
	corpusDir, evalDir := fixture(t)
	glx := buildIndexFile(t, corpusDir)
	code, out, _ := run("check", "--index", glx, "--format", "markdown", evalDir)
	if code != ExitOK || !strings.Contains(out, "| Document | Contamination |") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestCheckJSONLEvalWithCustomField(t *testing.T) {
	corpusDir, _ := fixture(t)
	glx := buildIndexFile(t, corpusDir)
	dir := t.TempDir()
	eval := write(t, dir, "eval.jsonl",
		`{"question":"The mitochondria is the powerhouse of the cell and produces energy for the organism.","id":1}
{"question":"What color is the sky on a clear day at noon over the ocean today then?","id":2}
`)
	code, out, _ := run("check", "--index", glx, "--field", "question", "--threshold", "50", eval)
	if code != ExitOK {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if !strings.Contains(out, eval+":1") {
		t.Fatalf("flagged record id missing:\n%s", out)
	}
	if !strings.Contains(out, "flagged  1 of 2 documents") {
		t.Fatalf("summary wrong:\n%s", out)
	}
}

func TestCheckLineSplitMode(t *testing.T) {
	corpusDir, _ := fixture(t)
	glx := buildIndexFile(t, corpusDir)
	dir := t.TempDir()
	eval := write(t, dir, "qs.txt",
		"The mitochondria is the powerhouse of the cell and produces energy for the organism.\n"+
			"Name a famous bridge in a European capital city crossing a large river somewhere.\n")
	code, out, _ := run("check", "--index", glx, "--split", "line", eval)
	if code != ExitOK || !strings.Contains(out, eval+":1") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestCheckEvalInputErrorsExit3(t *testing.T) {
	corpusDir, _ := fixture(t)
	glx := buildIndexFile(t, corpusDir)
	code, _, _ := run("check", "--index", glx, filepath.Join(t.TempDir(), "nope"))
	if code != ExitRuntime {
		t.Fatalf("missing path: code=%d", code)
	}
	code, _, errOut := run("check", "--index", glx, t.TempDir())
	if code != ExitRuntime || !strings.Contains(errOut, "no eval documents") {
		t.Fatalf("empty eval set: code=%d err=%q", code, errOut)
	}
}

func TestCheckAllFlagListsCleanDocuments(t *testing.T) {
	corpusDir, evalDir := fixture(t)
	glx := buildIndexFile(t, corpusDir)
	code, out, _ := run("check", "--index", glx, "--all", evalDir)
	if code != ExitOK || !strings.Contains(out, "clean.txt") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestCheckCustomNWithAgainst(t *testing.T) {
	corpusDir, evalDir := fixture(t)
	code, out, _ := run("check", "--against", corpusDir, "-n", "4", evalDir)
	if code != ExitOK || !strings.Contains(out, "n=4") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestIndexAndCheckAreDeterministicAcrossRuns(t *testing.T) {
	corpusDir, evalDir := fixture(t)
	glxA := buildIndexFile(t, corpusDir)
	glxB := buildIndexFile(t, corpusDir)
	a, err := os.ReadFile(glxA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(glxB)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("index files differ between identical runs")
	}
	_, out1, _ := run("check", "--index", glxA, "--format", "json", evalDir)
	_, out2, _ := run("check", "--index", glxB, "--format", "json", evalDir)
	// The index path differs in the label; normalize it away.
	out1 = strings.ReplaceAll(out1, glxA, "IX")
	out2 = strings.ReplaceAll(out2, glxB, "IX")
	if out1 != out2 {
		t.Fatal("check reports differ between identical runs")
	}
}
