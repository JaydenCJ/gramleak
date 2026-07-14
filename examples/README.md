# gramleak examples

Two runnable scripts, both offline and idempotent.

## `make-demo-data.sh`

Fabricates the dataset used in the main README's Quickstart: a small
training corpus (`corpus/train.jsonl` + `corpus/fewshot.txt`) and an eval
set (`eval/questions.jsonl`) in which `q1` and `q2` leak from the corpus.

```bash
bash examples/make-demo-data.sh /tmp/gramleak-demo
gramleak index --out corpus.glx /tmp/gramleak-demo/corpus
gramleak check --index corpus.glx --field question /tmp/gramleak-demo/eval
```

## `ci-gate.sh`

A pre-release leak gate for a build pipeline: writes the JSON report to
`leak-report.json` and fails the step (exit 1) when any eval document is
at least 30% contaminated.

```bash
bash examples/ci-gate.sh /tmp/gramleak-demo/eval corpus.glx
```

Both scripts assume `gramleak` is on `PATH` (or adjust to `./gramleak`
after `go build -o gramleak ./cmd/gramleak`).
