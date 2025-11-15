FROM golang:1.25-trixie AS builder

WORKDIR /build

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -o pocketbase-ha .

# Production image
FROM debian:trixie-slim AS production

LABEL org.opencontainers.image.source=https://github.com/litesql/pocketbase-ha

RUN groupadd --system --gid 1000 ha && \
    useradd --system --uid 1000 --gid 1000 --home /data ha

RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app
RUN mkdir -p /app/pb_data && chown -R ha:ha /app/pb_data

VOLUME /app/pb_data

COPY --from=builder /build/pocketbase-ha .
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

EXPOSE 4222 6222 8090

USER ha

ENV PB_NATS_STORE_DIR="/app/pb_data"

ENTRYPOINT ["/app/entrypoint.sh"]