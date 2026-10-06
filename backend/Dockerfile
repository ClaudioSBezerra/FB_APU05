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
RUN apk --no-cache add tzdata ca-certificates && \
    ln -sf /usr/share/zoneinfo/America/Sao_Paulo /etc/localtime
WORKDIR /root/
COPY --from=builder /app/server .
COPY --from=builder /app/migrations ./migrations
EXPOSE 8081
CMD ["./server"]
