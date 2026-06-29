# syntax=docker/dockerfile:1

# --- build stage ------------------------------------------------------------
# Pure-Go build (modernc.org/sqlite is cgo-free), so CGO_ENABLED=0 yields a
# single static binary. The committed *_templ.go and embedded static assets mean
# no templ/node tooling is needed here.
FROM golang:1.25-alpine AS build
WORKDIR /src

# Cache deps first.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mend ./cmd/server

# --- runtime stage ----------------------------------------------------------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget && mkdir -p /data
WORKDIR /app
COPY --from=build /out/mend /app/mend

# Default the DB + blob storage to /data so mounting a Railway volume at /data
# "just works" with no extra env. Override with DB_PATH / DATA_DIR if you like.
ENV DB_PATH=/data/mend.db \
    DATA_DIR=/data

EXPOSE 8080

# Local convenience; Railway uses its own healthcheckPath (see railway.json).
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD wget -qO- "http://127.0.0.1:${PORT:-8080}/healthz" || exit 1

ENTRYPOINT ["/app/mend"]
