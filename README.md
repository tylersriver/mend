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
charts. Prescription (`protocols` / `protocol_exercises`) is deferred per the
design — this ships `exercises → sessions → sets → measurements`.

**Phase 4 (PWA shell) is built.** The app is installable: a web manifest, maskable
icons, and a **deliberately minimal** service worker. Per the design, the worker
caches only the app shell + a static `/offline` page and **never caches dynamic
responses** — htmx fragments and all medical data always hit the network (stale
health data is worse than an error). Service workers require HTTPS in production
(localhost is exempt for dev).

All four planned phases are now built:

1. ~~**Core loop:** resources + providers + appointments + AI question generator~~ ✅
2. ~~**Recordings:** MediaRecorder upload → server-side transcription → AI highlights/tasks~~ ✅
3. ~~**PT logging:** the satellite~~ ✅ (protocols/prescription still deferred)
4. ~~**PWA shell:** manifest + service worker~~ ✅

## Quickstart

```sh
export ANTHROPIC_API_KEY=sk-ant-...   # optional; AI flows disable without it
export TRANSCRIBE_API_KEY=sk-...      # optional; audio transcription disables without it
go run ./cmd/server                   # serves http://localhost:8080
```

The templ views are compiled to committed `*_templ.go` files, so a plain
`go build ./...` works with no extra tooling. If you edit a `.templ`, regenerate
with `go run github.com/a-h/templ/cmd/templ@latest generate`.

Config (env, with `-flag` overrides): `ADDR` (default `:8080`), `DB_PATH`
(`data/mend.db`), `DATA_DIR` (`data`), `AI_MODEL` (`claude-opus-4-8`).
Transcription (OpenAI-compatible): `TRANSCRIBE_API_KEY`, `TRANSCRIBE_BASE_URL`
(default `https://api.openai.com/v1`), `TRANSCRIBE_MODEL` (default `whisper-1`).

## Security notes

This DB holds medical information. Keep the API key in env (not a SQLite row),
serve over HTTPS (service workers require it), and put the app behind Tailscale or
Cloudflare Access if it's internet-reachable. Encrypt audio recordings at rest;
let yourself delete audio once a transcript exists.
