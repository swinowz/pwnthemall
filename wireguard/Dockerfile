FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build the management API
RUN CGO_ENABLED=0 GOOS=linux go build -o /wg-manager ./cmd/manager

# Final image
FROM alpine:3.19

# Install WireGuard and dependencies
RUN apk add --no-cache \
    wireguard-tools \
    iptables \
    ip6tables \
    iproute2 \
    bash \
    curl \
    jq

# Copy the management binary
COPY --from=builder /wg-manager /usr/local/bin/wg-manager

# Copy entrypoint script
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Create config directory
RUN mkdir -p /etc/wireguard

# Expose WireGuard UDP port and management API port
EXPOSE 51820/udp 8080/tcp

# Environment variables
ENV WG_INTERFACE=wg0
ENV WG_PORT=51820
ENV WG_ADDRESS=10.13.0.1/16
ENV API_PORT=8080

ENTRYPOINT ["/entrypoint.sh"]
