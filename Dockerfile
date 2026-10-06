# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 pure Go compilation
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o finder ./cmd/finder

# Production runtime stage
FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

# Create data directory for SQLite persistence
RUN mkdir -p /app/data

COPY --from=builder /app/finder /app/finder
COPY --from=builder /app/.env.example /app/.env.example

VOLUME ["/app/data"]

ENV DB_PATH=/app/data/connectclip.db
ENV SCAN_INTERVAL=30m
ENV HTTP_ENABLED=true
ENV HTTP_ADDR=:8080

EXPOSE 8080

ENTRYPOINT ["/app/finder"]
