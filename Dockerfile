FROM golang:1.24-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /autovpn ./cmd/autovpn

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl iproute2 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /autovpn /usr/local/bin/autovpn

ENTRYPOINT ["autovpn"]
