#!/usr/bin/env bash
# Fabricates a small demo dataset: a training corpus plus an eval set in
# which two questions leak verbatim (or near-verbatim) from the corpus.
# Usage: bash examples/make-demo-data.sh [target-dir]   (default: ./demo-data)
set -euo pipefail

TARGET="${1:-./demo-data}"
mkdir -p "$TARGET/corpus" "$TARGET/eval"

cat > "$TARGET/corpus/train.jsonl" <<'EOF'
{"text": "The Great Barrier Reef is the world's largest coral reef system, composed of over 2,900 individual reefs and 900 islands stretching for over 2,300 kilometres."}
{"text": "Mount Everest, standing at 8,849 metres above sea level, is Earth's highest mountain, located in the Mahalangur Himal sub-range of the Himalayas."}
{"text": "Photosynthesis is the process by which green plants convert sunlight, water and carbon dioxide into oxygen and glucose inside their chloroplasts."}
{"text": "The Turing test, proposed in 1950, measures a machine's ability to exhibit intelligent behaviour indistinguishable from that of a human."}
{"text": "Honey never spoils because its low moisture content and acidic pH create an environment where bacteria cannot survive for long."}
EOF

cat > "$TARGET/corpus/fewshot.txt" <<'EOF'
Example 1
Q: What is the largest coral reef system in the world?
A: The Great Barrier Reef, which stretches for over 2,300 kilometres off the coast of Australia.

Example 2
Q: Why does honey never spoil?
A: Its low moisture content and acidic pH create an environment where bacteria cannot survive for long.
EOF

cat > "$TARGET/eval/questions.jsonl" <<'EOF'
{"id": "q1", "question": "Photosynthesis is the process by which green plants convert sunlight, water and carbon dioxide into oxygen and glucose inside their chloroplasts. True or false?"}
{"id": "q2", "question": "According to the passage, the Turing test, proposed in 1950, measures a machine's ability to exhibit intelligent behaviour. Discuss briefly."}
{"id": "q3", "question": "Which planet in our solar system has the strongest sustained winds ever recorded at its cloud tops, and roughly how fast do they blow?"}
{"id": "q4", "question": "Name the chemical element with the highest melting point and give one industrial application that depends on that property."}
{"id": "q5", "question": "In which year did the first transatlantic radio transmission take place, and which two locations did it connect?"}
{"id": "q6", "question": "What determines the direction of a comet's tail relative to the sun as the comet travels through the inner solar system?"}
EOF

echo "demo data written to $TARGET (corpus: 2 files, eval: 6 questions, 2 leaked)"
