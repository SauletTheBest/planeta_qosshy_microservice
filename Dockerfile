# Qosshy Pay — test payment service for Planeta Qosshy

# Stage 1: build
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# templates/ are embedded into the binary (go:embed), so nothing else needs copying later
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o payments .

# Stage 2: small runtime image
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -s /sbin/nologin -u 10001 app
WORKDIR /app
COPY --from=builder /app/payments .
# data/ keeps payments.json, receipts/ the PDF receipts (both are volumes in docker-compose)
RUN mkdir -p /app/data /app/receipts && chown app /app/data /app/receipts
USER app
ENV GIN_MODE=release DATA_FILE=/app/data/payments.json
EXPOSE 8081
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8081/healthz || exit 1
CMD ["./payments"]
