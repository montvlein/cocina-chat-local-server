# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files first for caching
COPY go.mod go.sum ./

# Download dependencies and update go.sum
RUN go mod tidy && go mod download

# Copy source code
COPY . .

# Build the application (no CGO needed with modernc.org/sqlite)
RUN GOOS=linux go build -o cocina-server ./cmd/server/

# Final stage
FROM alpine:3.18

WORKDIR /app

# Install SQLite dependencies
RUN apk add --no-cache sqlite-libs ca-certificates

# Copy binary from builder
COPY --from=builder /app/cocina-server .

# Create data directory
RUN mkdir -p /data

EXPOSE 8090

CMD ["./cocina-server"]
