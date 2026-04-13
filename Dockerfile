# ── Build stage ───────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

ARG VERSION=dev

WORKDIR /build

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o ebay-watcher .

# ── Final stage ───────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /build/ebay-watcher /ebay-watcher

EXPOSE 8080

ENTRYPOINT ["/ebay-watcher"]
