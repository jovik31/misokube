# syntax=docker/dockerfile:1
FROM golang:1.24    AS builder

ARG CMD_PATH
WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download
COPY . .


# Dinamically build the binary based on the passed CMD_PATH (e.g. cmd/orchestrator or cmd/daemon)
RUN CGO_ENABLED=0 GOOS=linux go build -a -o app ./${CMD_PATH}

FROM gcr.io/distroless/static:nonroot

WORKDIR /
COPY --from=builder /workspace/app /app

USER nonroot:nonroot
ENTRYPOINT ["/app"]