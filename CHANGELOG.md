# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-07-13

### Added

- `index` subcommand: streams any mix of text files, directories and
  JSONL datasets into a deduplicated set of hashed token n-grams and
  writes it as a compact `.glx` file (GLXI v1: sorted 64-bit FNV-1a
  fingerprints, embedded shingling parameters, trailing checksum).
- `check` subcommand: measures per-document contamination (token coverage
  by corpus n-grams), reconstructs contiguous overlap runs from byte
  offsets and quotes them verbatim as evidence, with `--threshold`
  flagging and deterministic worst-first ordering.
- `--fail-over` release gate: exit code 1 the moment any eval document
  reaches the limit, for pre-release pipelines and hooks; usage errors
  exit 2, runtime errors 3.
- Three report formats: terminal gauges, stable JSON
  (`schema_version: 1`, two-decimal rounding, byte-identical re-runs) and
  PR-ready Markdown tables with an evidence section.
- Input handling: recursive deterministic directory walks (dot-entries
  skipped), `--split file|line|para` for plain text, `--field` with
  dotted paths for `.jsonl`/`.ndjson`, binary-file sniffing, and hard
  errors (never silent skips) on malformed dataset records.
- Normalization options recorded in the index and enforced at check time:
  case folding (default), `--case-sensitive`, and `--mask-digits` for
  templated contamination; parameter overrides against an index file are
  rejected as usage errors.
- `--against` mode for one-shot checks without writing an index, plus a
  `stats` subcommand to inspect `.glx` files.
- Runnable examples (`examples/make-demo-data.sh`, `examples/ci-gate.sh`)
  and an index format reference (`docs/index-format.md`).
- 89 deterministic offline tests (unit + in-process CLI integration
  against fabricated datasets) and `scripts/smoke.sh`.

[0.1.0]: https://github.com/JaydenCJ/gramleak/releases/tag/v0.1.0
