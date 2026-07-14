# Contributing to gramleak

Issues, discussions and pull requests are all welcome.

## Getting started

You need Go ≥1.22; nothing else — the tool and its tests are stdlib-only.

```bash
git clone https://github.com/JaydenCJ/gramleak && cd gramleak
go build ./...
go test ./...
bash scripts/smoke.sh
```

`scripts/smoke.sh` builds the binary, fabricates a deterministic corpus
and eval set in a temp dir, and asserts on real CLI output across every
subcommand and exit code; it must finish by printing `SMOKE OK`.

## Before you open a pull request

1. `gofmt -l .` reports nothing (formatting is enforced).
2. `go vet ./...` passes with no findings.
3. `go test ./...` passes (89 deterministic tests, no network).
4. `bash scripts/smoke.sh` prints `SMOKE OK`.
5. Add tests for behavior changes; keep logic in pure, unit-testable
   modules (only `internal/cli` and `internal/corpus` touch the OS).

## Ground rules

- Keep dependencies at zero; adding one needs strong justification in
  the PR.
- No network calls, ever, and no telemetry — gramleak reads local files
  and writes local reports, full stop.
- The `.glx` format is a contract: any change bumps the format version,
  keeps `Read` accepting nothing it cannot verify, and updates
  `docs/index-format.md` in the same PR.
- Determinism first: identical input must produce byte-identical reports
  and index files, including all orderings.
- Code comments and doc comments are written in English.

## Reporting bugs

Include the output of `gramleak version`, the full command you ran, the
report output, and — for miscounts — a minimal corpus/eval pair that
reproduces the numbers (a few lines of JSONL is usually enough, since
classification depends only on tokenized n-grams).

## Security

Please do not open public issues for security problems; use GitHub's
private vulnerability reporting on this repository instead.
