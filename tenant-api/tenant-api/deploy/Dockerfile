FROM golang:1.23 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o tenant-api ./main.go

# ---

FROM alpine:latest

ARG VCLUSTER_VERSION=v0.36.0

RUN apk add --no-cache bash ca-certificates curl helm

# Install vcluster CLI
RUN curl -fL -o /usr/local/bin/vcluster \
      "https://github.com/loft-sh/vcluster/releases/download/${VCLUSTER_VERSION}/vcluster-linux-amd64" && \
    chmod +x /usr/local/bin/vcluster

# Install kubectl
RUN curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && \
    install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl && \
    rm kubectl

WORKDIR /
COPY --from=builder /app/tenant-api .

RUN addgroup -S tenant-api && adduser -S -G tenant-api tenant-api && \
    mkdir -p /home/tenant-api/.cache /home/tenant-api/.config && \
    chown -R tenant-api:tenant-api /home/tenant-api

ENV HOME=/home/tenant-api
USER tenant-api

EXPOSE 8080

ENTRYPOINT ["/tenant-api"]
