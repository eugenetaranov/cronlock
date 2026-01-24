# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /src

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /cronlock ./cmd/cronlock

# Runtime stage
FROM alpine:3.19

# Install ca-certificates for HTTPS and tzdata for timezone support
RUN apk add --no-cache ca-certificates tzdata

# Copy binary from builder
COPY --from=builder /cronlock /usr/local/bin/cronlock

# Create non-root user
RUN adduser -D -H cronlock
USER cronlock

ENTRYPOINT ["cronlock"]
CMD ["-config", "/etc/cronlock/config.yaml"]
