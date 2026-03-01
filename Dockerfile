FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG VERSION=dev
RUN go build -ldflags="-X main.version=${VERSION}" -o /bin/api ./cmd/api

# ── runtime ──────────────────────────────────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /bin/api /bin/api

EXPOSE 8080
ENTRYPOINT ["/bin/api"]
