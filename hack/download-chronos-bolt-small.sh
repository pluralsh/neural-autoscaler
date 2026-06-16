#!/usr/bin/env bash
# Deprecated: use hack/download-chronos-onnx.sh (downloads Chronos-2 ONNX, not bolt-small).
set -euo pipefail
exec "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/download-chronos-onnx.sh" "$@"
