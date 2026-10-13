# Build Stage
FROM golang:1.22-alpine AS builder
WORKDIR /app

# Copy go.mod and go.sum from the backend directory
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# Copy the rest of the backend source code
COPY backend/ .

# Build the binary (sem vendor — go mod download já resolve as dependências)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o server .

# Run Stage
FROM alpine:latest
# curl: só para o HEALTHCHECK do docker-compose.prod.yml (GET /api/health)
RUN apk --no-cache add tzdata ca-certificates curl && \
    ln -sf /usr/share/zoneinfo/America/Sao_Paulo /etc/localtime
WORKDIR /root/
COPY --from=builder /app/server .
COPY --from=builder /app/migrations ./migrations
EXPOSE 8081
HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
    CMD curl -f http://localhost:8081/api/health || exit 1
CMD ["./server"]
