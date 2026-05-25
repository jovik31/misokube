# syntax=docker/dockerfile:1.7

FROM golang:1.25-bookworm AS builder

# Install clang/llvm for BPF compilation and libbpf headers
RUN apt-get update && \
    apt-get install -y clang llvm libbpf-dev gcc-multilib && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Generate BPF Go bindings and build the firewall binary
RUN go generate ./pkg/firewall/...
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /firewall ./cmd/firewall/main.go

FROM alpine:latest
RUN apk update && apk add --no-cache iproute2
COPY --from=builder /firewall /firewall
ENTRYPOINT ["/firewall"]
