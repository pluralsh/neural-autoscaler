#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODEL_DIR="${ROOT_DIR}/models/timesfm-2.5-onnx"
REPO="pdufour/timesfm-2.5-200m-transformers-onnx"

if command -v hf >/dev/null 2>&1; then
  HF_CMD=(hf)
elif [[ -x "${ROOT_DIR}/.venv/bin/hf" ]]; then
  HF_CMD=("${ROOT_DIR}/.venv/bin/hf")
elif command -v huggingface-cli >/dev/null 2>&1; then
  HF_CMD=(huggingface-cli)
else
  echo "hf (huggingface-cli) is required. Install with: pip install huggingface_hub" >&2
  exit 1
fi

mkdir -p "${MODEL_DIR}/onnx"
"${HF_CMD[@]}" download "${REPO}" onnx/model.onnx onnx/model.onnx.data --local-dir "${MODEL_DIR}"

echo "TimesFM ONNX model downloaded to ${MODEL_DIR}/onnx"
echo
echo "Run locally with:"
echo "  make run-local ONNX_RUNTIME_LIB_PATH=/path/to/libonnxruntime.so"
echo "Or:"
echo "  MODEL_PATH=${MODEL_DIR}/onnx/model.onnx go run ./cmd/main.go --model-path=${MODEL_DIR}/onnx/model.onnx"
