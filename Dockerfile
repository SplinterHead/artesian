# ==========================================
# Stage 1: Build Frontend (VueJS + Vite)
# ==========================================
FROM node:22-alpine AS frontend-builder

WORKDIR /build/web

COPY web/package.json ./
RUN npm install

COPY web/ ./
RUN npm run build

# ==========================================
# Stage 2: Build Backend (Go + Embedded UI)
# ==========================================
FROM golang:1.24-alpine AS backend-builder

RUN apk add --no-cache gcc musl-dev libc-dev

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Copy compiled frontend from Stage 1 for Go embed.FS
COPY --from=frontend-builder /build/web/dist ./web/dist

# Build production binary with CGO enabled for SQLite
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /build/artesian ./cmd/artesian

# ==========================================
# Stage 3: Minimal Runtime Image
# ==========================================
FROM alpine:3.21 AS runner

RUN apk add --no-cache ca-certificates tzdata

# Create working data directories
RUN mkdir -p /data/cache /data/manifests

# Copy compiled binary from builder
COPY --from=backend-builder /build/artesian /usr/local/bin/artesian

# DNS (53 udp/tcp), HTTP Proxy (80 tcp), HTTPS SNI Proxy (443 tcp), Web UI & API (8080 tcp)
EXPOSE 53/udp 53/tcp 80 443 8080

VOLUME ["/data"]

WORKDIR /

ENTRYPOINT ["/usr/local/bin/artesian"]
CMD ["-db", "/data/artesian.db"]
