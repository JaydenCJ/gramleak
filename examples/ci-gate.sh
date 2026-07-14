#!/usr/bin/env bash
# Pre-release leak gate: refuse to ship an eval set if any document is
# ≥ 30% contaminated by the training or few-shot corpus.
#
# Usage: bash examples/ci-gate.sh <eval-dir-or-file> <corpus.glx>
# Wire it into a release checklist or a pre-push hook; gramleak exits 1 on
# breach, so `set -e` (or a plain `if`) stops the pipeline.
set -euo pipefail

EVAL="${1:?usage: ci-gate.sh <eval-path> <index.glx>}"
INDEX="${2:?usage: ci-gate.sh <eval-path> <index.glx>}"

# Machine-readable report for the build artifact, human verdict on stderr.
# --field question matches the demo eval set; point it at your dataset's
# text field.
if gramleak check --index "$INDEX" --field question \
    --threshold 10 --fail-over 30 --format json "$EVAL" > leak-report.json; then
  echo "leak gate: PASS (report in leak-report.json)"
else
  status=$?
  if [ "$status" -eq 1 ]; then
    echo "leak gate: FAIL — inspect leak-report.json before shipping" >&2
  else
    echo "leak gate: gramleak error (exit $status), see the message above" >&2
  fi
  exit "$status"
fi
