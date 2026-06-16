#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODEL_DIR="${ROOT_DIR}/models/chronos-2-onnx"
# Community Chronos-2 ONNX export with future-covariate tensor interface.
# amazon/chronos-bolt-small has no public ONNX weights; this is Chronos-2 (~457 MB).
REPO="${CHRONOS_ONNX_REPO:-TSFM-ai/chronos-2-onnx}"

if command -v hf >/dev/null 2>&1; then
  HF_CMD=(hf)
elif [[ -x "${ROOT_DIR}/.venv/bin/hf" ]]; then
  HF_CMD=("${ROOT_DIR}/.venv/bin/hf")
elif command -v huggingface-cli >/dev/null 2>&1; then
  HF_CMD=(huggingface-cli)
else
  echo "hf (huggingface-cli) is required. Install with:" >&2
  echo "  python3 -m venv .venv && .venv/bin/pip install huggingface_hub" >&2
  exit 1
fi

mkdir -p "${MODEL_DIR}"
"${HF_CMD[@]}" download "${REPO}" \
  model.onnx \
  config.json \
  generation_config.json \
  --local-dir "${MODEL_DIR}"

echo "Chronos-2 ONNX model downloaded to ${MODEL_DIR}"
echo "  model:  ${MODEL_DIR}/model.onnx"
echo "  config: ${MODEL_DIR}/config.json"
echo
echo "Source repo: ${REPO}"
echo "Run locally with:"
echo "  MODEL_PATH=${MODEL_DIR}/model.onnx go run ./cmd/main.go \\"
echo "    --model-path=${MODEL_DIR}/model.onnx --model-family=chronos2 \\"
echo "    --onnx-runtime-lib-path=/path/to/libonnxruntime.so"
