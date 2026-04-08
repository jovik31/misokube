# syntax=docker/dockerfile:1.7

#Build stage
FROM golang:1.25-bookworm



FROM golang:1.25 AS builder

ARG BINARY
WORKDIR /setera

COPY go.mod go.sum ./
RUN go mod download
COPY . .


# Dinamically build the binary based on the passed CMD_PATH (e.g. cmd/orchestrator or cmd/daemon)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/${BINARY} ./cmd/${BINARY}/main.go

FROM alpine:latest
RUN apk update && apk add --no-cache iptables

ARG BINARY
WORKDIR /
COPY --from=builder /setera/bin/${BINARY} /
