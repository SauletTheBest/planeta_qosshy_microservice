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
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/payments .
EXPOSE 8081
CMD ["./payments"]
