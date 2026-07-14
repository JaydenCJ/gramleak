#!/usr/bin/env bash
# End-to-end smoke test for gramleak: builds the binary, fabricates a
# deterministic corpus + eval set in a temp dir, and asserts on the real
# CLI output of every subcommand. No network, idempotent, finishes in
# seconds.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

fail() {
  echo "SMOKE FAIL: $*" >&2
  exit 1
}

BIN="$WORKDIR/gramleak"
GLX="$WORKDIR/corpus.glx"

echo "1. build"
(cd "$ROOT" && go build -o "$BIN" ./cmd/gramleak) || fail "go build failed"

echo "2. version matches manifest"
"$BIN" version | grep -qx "gramleak 0.1.0" || fail "version mismatch"

echo "3. fabricate demo corpus and eval set"
bash "$ROOT/examples/make-demo-data.sh" "$WORKDIR/data" >/dev/null

echo "4. index the corpus"
OUT="$("$BIN" index --out "$GLX" "$WORKDIR/data/corpus")"
echo "$OUT" | grep -q "indexed 6 documents" || fail "corpus doc count wrong"
echo "$OUT" | grep -q "119 shingles" || fail "shingle count wrong"
[ -f "$GLX" ] || fail "index file not written"

echo "5. stats reads the index back"
"$BIN" stats "$GLX" | grep -q "GLXI v1" || fail "stats format line missing"
"$BIN" stats "$GLX" | grep -q "shingles   119" || fail "stats shingles wrong"

echo "6. check flags the leaked questions with evidence"
REPORT="$("$BIN" check --index "$GLX" --field question "$WORKDIR/data/eval")"
echo "$REPORT" | grep -q "flagged  2 of 6 documents" || fail "flag count wrong"
echo "$REPORT" | grep -q "questions.jsonl:1" || fail "leaked q1 not reported"
echo "$REPORT" | grep -q "longest run 21 tokens" || fail "longest run wrong"
echo "$REPORT" | grep -q "Photosynthesis is the process" || fail "evidence quote missing"
echo "$REPORT" | grep -q "█" || fail "gauge missing"
echo "$REPORT" | grep -q "questions.jsonl:3" && fail "clean question listed"

echo "7. JSON report is machine-readable and correct"
JSON="$("$BIN" check --index "$GLX" --field question --format json "$WORKDIR/data/eval")"
echo "$JSON" | grep -q '"tool": "gramleak"' || fail "json envelope missing"
echo "$JSON" | grep -q '"flagged": 2' || fail "json flag count wrong"
echo "$JSON" | grep -q '"max_pct": 87.5' || fail "json max pct wrong"

echo "8. fail-over gate enforces exit codes"
"$BIN" check --index "$GLX" --field question --fail-over 95 \
  "$WORKDIR/data/eval" >/dev/null || fail "gate should pass at 95%"
if "$BIN" check --index "$GLX" --field question --fail-over 30 \
    "$WORKDIR/data/eval" >/dev/null; then
  fail "gate should breach at 30% (worst doc is 87.5%)"
fi

echo "9. --against needs no index file"
"$BIN" check --against "$WORKDIR/data/corpus" --corpus-field text \
  --field question "$WORKDIR/data/eval" \
  | grep -q "in-memory" || fail "--against mode broken"

echo "10. usage errors exit 2"
set +e
"$BIN" check --index "$GLX" -n 5 --field question "$WORKDIR/data/eval" >/dev/null 2>&1
[ $? -eq 2 ] || fail "param override with --index should exit 2"
"$BIN" check --index "$GLX" --format yaml "$WORKDIR/data/eval" >/dev/null 2>&1
[ $? -eq 2 ] || fail "bad --format should exit 2"
set -e

echo "11. corrupt index exits 3, loudly"
printf 'GLXI this file has been mangled beyond recognition....' > "$WORKDIR/bad.glx"
set +e
"$BIN" stats "$WORKDIR/bad.glx" >/dev/null 2>&1
[ $? -eq 3 ] || fail "corrupt index should exit 3"
set -e

echo "SMOKE OK"
