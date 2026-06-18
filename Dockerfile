# Manager image: Kubernetes controller with bundled ONNX Runtime and Chronos-2 ONNX model.
#
# Prerequisites:
#   make download-chronos-onnx   # downloads models/chronos-2-onnx/model.onnx (~457MB)
#
# Build:
#   make docker-build
#   docker build -t controller:latest .
#
# Run locally (requires kubeconfig):
#   docker run --rm \
#     -v "$HOME/.kube/config:/etc/kubeconfig:ro" \
#     -e KUBECONFIG=/etc/kubeconfig \
#     controller:latest \
#     --leader-elect
#
# Environment (forecast flags also accept CLI equivalents):
#   MODEL_PATH                Path to model.onnx (default: /models/chronos-2-onnx/model.onnx)
#   FORECAST_MODEL_FAMILY     timesfm | chronos2 (optional; auto-discovered when unset)
#   ONNX_RUNTIME_LIB_PATH     libonnxruntime.so (default: /opt/onnxruntime/lib/libonnxruntime.so)
#   ONNX_RUNTIME_API_VERSION  ORT C API version for purego (default: 23)

# syntax=docker/dockerfile:1

ARG GO_VERSION=1.26.4
ARG ONNXRUNTIME_VERSION=1.24.4

FROM golang:${GO_VERSION}-bookworm AS builder
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/manager cmd/main.go

FROM debian:bookworm-slim AS ort
ARG ONNXRUNTIME_VERSION

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && curl -fsSL \
      "https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}.tgz" \
      | tar -xz -C /opt \
    && mv "/opt/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}" /opt/onnxruntime \
    && apt-get purge -y curl \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/*

FROM debian:bookworm-slim AS manager

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates \
      libstdc++6 \
      libgomp1 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=ort /opt/onnxruntime/lib/ /opt/onnxruntime/lib/
COPY --from=builder /out/manager /manager
COPY models/chronos-2-onnx/model.onnx /models/chronos-2-onnx/model.onnx

ENV LD_LIBRARY_PATH=/opt/onnxruntime/lib \
    ONNX_RUNTIME_LIB_PATH=/opt/onnxruntime/lib/libonnxruntime.so \
    ONNX_RUNTIME_API_VERSION=23 \
    MODEL_PATH=/models/chronos-2-onnx/model.onnx

ENTRYPOINT ["/manager"]
CMD ["--leader-elect"]
