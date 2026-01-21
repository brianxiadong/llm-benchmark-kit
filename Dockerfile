# Multi-stage build for minimal image size
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build static binary
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o llm-benchmark-kit ./cmd/llm-benchmark-kit

# Final stage
FROM alpine:3.19

# Install CA certificates for HTTPS
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/llm-benchmark-kit /usr/local/bin/

# Create output directory
RUN mkdir -p /app/output

ENTRYPOINT ["llm-benchmark-kit"]
CMD ["--help"]
