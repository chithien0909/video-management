FROM golang:1.25.1 AS builder

WORKDIR /app

# Install ffmpeg for build-time tests if needed (optional)
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the application from the new entry point
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o server ./cmd/movie

FROM debian:bookworm-slim

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    ffmpeg \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/server /app/server

# Prepare writable dirs for bind mounts or container storage
RUN mkdir -p /app/uploads /app/hls \
    && groupadd -r app && useradd -r -g app app \
    && chown -R app:app /app

EXPOSE 8080
USER app:app
ENTRYPOINT ["/app/server"]