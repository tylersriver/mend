# Shoulder Tracker

A mobile-first, installable (PWA) personal app for managing **one injury** end to
end: diagnosis, research, clinician appointments, an AI care-prep copilot, and
(secondarily) PT progress.

This is **not** a workout app. The center of gravity is *prognosis, planning, and
not losing the thread across months of appointments* — the gap the workout-app
market doesn't fill. See [`docs/DESIGN.md`](docs/DESIGN.md) for the full decision
log and rationale.

## Stack

- **Backend:** Go (stdlib `net/http`, 1.22+ method/path routing — no chi/gorilla)
- **DB:** SQLite (blobs on disk, metadata in DB)
- **UI:** templ + htmx, Tailwind — server-rendered, no SPA, no build pipeline
- **Config/CLI:** viper (env-first); `flag` likely enough, cobra optional
- **AI:** Anthropic Messages API, called server-side from Go (no SDK)
- **PWA:** manifest + icons + a *minimal* service worker (app-shell cache only —
  intentionally **no offline data**; installable + online is the target)

## Status

**Phase 1 (core loop) is built and runnable.** What's here:

- `docs/DESIGN.md` — architecture decisions and rationale
- `db/migrations/` — the SQLite schema (PT tracking + research/care + a single-row
  case profile in `0003`)
- `internal/ai/` — the Anthropic client and the four care-prep flows
- `internal/config/` — env-first config (`flag` overrides; secrets stay in env)
- `internal/db/` — opens SQLite (pure-Go `modernc.org/sqlite`, cgo-free) and runs
  the embedded migrations on boot
- `internal/store/` — typed CRUD; also implements `ai.Repo` so the AI flows read
  the case file and write back editable artifacts
- `internal/web/` — `net/http` 1.22 routing, templ + htmx views (Tailwind via CDN)
- `cmd/server/` — wires it all together into one binary

The working core loop: a **case profile**, **research library** (link/video/file
with an AI summarize action), **providers**, **appointments** (prep vs. outcome),
and the **AI question generator** that drafts questions for an appointment grounded
in the case file. AI flows degrade gracefully when `ANTHROPIC_API_KEY` is unset.

**Phase 2 (recordings) is built.** Record or upload appointment audio; it's
transcribed server-side (a Whisper-class API), then Claude turns the transcript
into highlights + a task list. The browser only records (MediaRecorder) and
uploads — transcription never runs client-side. Pipeline state
(`pending → processing → done → failed`) is polled over htmx. Audio can be deleted
once a transcript exists (the transcript is kept).

**Phase 3 (PT logging satellite) is built.** Under the **PT** nav entry:
exercises (with a metric type that drives which set inputs show), sessions with
inline htmx set logging, and a tall `measurements` log with inline SVG trend
charts. **Prescription** (`protocols` / `protocol_exercises`) is also now built:
rehab phases with prescribed targets, new sessions auto-linked to the active
phase, and a plan-vs-performed compliance view (each prescribed target shown next
to the most recent set logged for it).

**Phase 4 (PWA shell) is built.** The app is installable: a web manifest, maskable
icons, and a **deliberately minimal** service worker. Per the design, the worker
caches only the app shell + a static `/offline` page and **never caches dynamic
responses** — htmx fragments and all medical data always hit the network (stale
health data is worse than an error). Service workers require HTTPS in production
(localhost is exempt for dev).

All four planned phases are now built:

1. ~~**Core loop:** resources + providers + appointments + AI question generator~~ ✅
2. ~~**Recordings:** MediaRecorder upload → server-side transcription → AI highlights/tasks~~ ✅
3. ~~**PT logging:** the satellite~~ ✅ (including prescription/compliance)
4. ~~**PWA shell:** manifest + service worker~~ ✅

**In-app settings & login (post-design).** AI/transcription credentials can be set
in-app at **/settings** (stored in the DB, applied live — no restart) instead of
only via env; env values act as a fallback. A single shared password gates the
whole app when `AUTH_PASSWORD` is set (signed session cookie); with it unset, auth
is off for local dev.

## Quickstart

```sh
cp .env.dist .env                     # then edit; or just export the vars you need
export ANTHROPIC_API_KEY=sk-ant-...   # optional; or set it in-app at /settings
export TRANSCRIBE_API_KEY=sk-...      # optional; audio transcription disables without it
go run ./cmd/server                   # serves http://localhost:8080
```

See [`.env.dist`](.env.dist) for every supported variable with defaults and notes.

The templ views are compiled to committed `*_templ.go` files, so a plain
`go build ./...` works with no extra tooling. If you edit a `.templ`, regenerate
with `go run github.com/a-h/templ/cmd/templ@latest generate`.

Config (env, with `-flag` overrides): `PORT` or `ADDR` (default `:8080`), `DB_PATH`
(`data/mend.db`), `DATA_DIR` (`data`), `AI_MODEL` (`claude-opus-4-8`).
Transcription (OpenAI-compatible): `TRANSCRIBE_API_KEY`, `TRANSCRIBE_BASE_URL`
(default `https://api.openai.com/v1`), `TRANSCRIBE_MODEL` (default `whisper-1`).
Auth: `AUTH_PASSWORD` (enables login), `SESSION_SECRET` (optional; stabilizes
session cookies across restarts — defaults to one derived from the password).

## Deploying (Railway)

This repo ships a `Dockerfile` and `railway.json`, so Railway builds the image
directly (multi-stage, CGO-free → a tiny static binary) and health-checks
`/healthz`.

1. Create a Railway project from this repo. `railway.json` selects the Dockerfile
   builder automatically; `PORT` is injected by Railway.
2. **Attach a volume mounted at `/data`.** The image already defaults
   `DB_PATH=/data/mend.db` and `DATA_DIR=/data`, so the SQLite DB and uploaded
   blobs persist there (Railway's container filesystem is otherwise ephemeral).
3. Set variables (see [`.env.dist`](.env.dist)): `AUTH_PASSWORD` (required for a
   private deploy) and `SESSION_SECRET` (`openssl rand -hex 32`). Optionally add
   `ANTHROPIC_API_KEY` / `TRANSCRIBE_API_KEY` — or set those in-app at `/settings`.
4. TLS is terminated at Railway's edge; the app detects HTTPS via
   `X-Forwarded-Proto` and marks the session cookie `Secure`.

To build/run the container locally:

```sh
docker build -t mend .
docker run --rm -p 8080:8080 -v "$PWD/data:/data" -e AUTH_PASSWORD=changeme mend
```

## Security notes

This DB holds medical information. Always set `AUTH_PASSWORD` before exposing the
app, and serve over HTTPS (service workers require it; the session cookie is marked
`Secure` behind a TLS-terminating proxy). For extra defense in depth you can still
put it behind Tailscale or Cloudflare Access.

The original design kept API keys in env only, never in the DB. The in-app
`/settings` screen relaxes that — keys you enter there are stored in the SQLite
file — an accepted tradeoff for a single-user, login-gated instance. If you'd
rather not persist keys in the DB, leave those fields blank and provide
`ANTHROPIC_API_KEY` / `TRANSCRIBE_API_KEY` via env instead. Either way, treat the
SQLite file (and any volume it lives on) as a secret. You can delete a recording's
audio once its transcript exists.
