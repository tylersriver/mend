# syntax=docker/dockerfile:1

# --- build stage ------------------------------------------------------------
# Pure-Go build (modernc.org/sqlite is cgo-free), so CGO_ENABLED=0 yields a
# single static binary. The committed *_templ.go and embedded static assets mean
# no templ/node tooling is needed here.
FROM golang:1.25-alpine AS build
RUN apk add --no-cache ca-certificates
WORKDIR /src

# Cache deps first.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mend ./cmd/server

# --- runtime stage ----------------------------------------------------------
# scratch: nothing but the static binary + CA bundle. The app creates its own
# /data and temp dirs at startup, and `mend -healthcheck` replaces the need for a
# shell-based HEALTHCHECK.
FROM scratch

# CA roots so outbound HTTPS (Anthropic / transcription) verifies.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/mend /mend

# Default the DB + blob storage to /data so mounting a Railway volume at /data
# "just works". Override with DB_PATH / DATA_DIR if you like.
ENV DB_PATH=/data/mend.db \
    DATA_DIR=/data

EXPOSE 8080

# Self-contained healthcheck (no shell in scratch). Railway also probes
# /healthz via railway.json.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD ["/mend", "-healthcheck"]

ENTRYPOINT ["/mend"]
