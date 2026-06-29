# Design & Decision Log

This captures *why* the app is shaped the way it is, so the reasoning survives
past the chat that produced it. Decisions are grouped by the question that forced
them.

## What this app is

A **personal medical case-file + research notebook with an AI copilot**, organized
around a single injury. The workout-app market starts from "log the workout";
almost nothing starts from "help me understand my diagnosis, prep for my doctor,
and not lose the thread across months of appointments." That gap is the niche.

PT logging is a **satellite**, not the spine. The spine is:
**resources + providers + appointments + AI question generator.**

## Stack decisions

**Go + SQLite + net/http + templ + htmx + Tailwind.**
Right "boring" choice for a single-user, CRUD-heavy app: no build pipeline to rot,
one binary to deploy, one file to back up.

- `net/http` only — the 1.22+ ServeMux does method + path routing, so no chi/gorilla.
- viper for config; cobra is likely overkill (a web server needs ~`serve` +
  `migrate`). `flag` + viper may be plenty.
- Pick a migration tool early (goose or golang-migrate, embedded SQL). SQLite
  schema changes are annoying without one.

## PWA / offline — the pivotal call

**Decision: "installable + online is fine."** No offline data.

Why it matters: htmx is server-driven — every interaction round-trips to render a
fragment. True offline would mean a local-first data layer (IndexedDB + a sync
queue) and a sync/conflict layer, which is the genuinely hard part — *not* the
rendering framework. Since offline isn't required, none of that is needed, and
**React/SPA would be a pure downgrade in simplicity** (reintroduces build tooling,
client state, demotes Go to a JSON API).

So "PWA" here is small and bolted on **last**:
- Web app manifest (name, icons, `theme_color`, `display: standalone`)
- Icons (192/512 PNG + iOS `apple-touch-icon`)
- A **minimal** service worker: cache the app shell + a static `/offline` page;
  let all htmx fragment requests hit the network. **Do not cache dynamic
  responses** — stale medical data is worse than an error.

Caveats accepted: iOS keeps PWAs second-class (storage eviction, flaky push);
service workers require HTTPS (localhost exempt for dev). "Wrap in native later"
via Capacitor/TWA is just a webview at the server — fine for distribution, but it
does **not** add offline.

## File / blob storage

Blobs on disk (`file_path`), metadata in SQLite. Never stuff files into the DB.
If "MRI scans" means raw DICOM (hundreds of MB, needs a viewer like cornerstone.js)
that's a separate concern; if it's the PDF report + exported JPEGs, ignore that.

## Data model decisions

### Research = one unified table, not files-vs-links
A YouTube video, a PubMed article, and an MRI PDF are the same *thing* to the user:
research to title, annotate, tag, and feed the AI. They differ only in storage, so
a `kind` discriminator (`file`/`link`/`video`) handles it. The `summary` column
doubles as the AI hook — summarize once, reuse cheaply as context forever.

### Appointments split prep from outcome
`prep_notes` (questions to raise, often AI-generated) vs `outcome` (what happened).
Over months, `outcome` across completed appointments *is* the clinical narrative of
the injury — the thing that's impossible to reconstruct from memory at month four.

### PT: prescription separate from performance
`protocol_exercises` (the plan: target sets/reps/load/RPE) is distinct from `sets`
(what was actually performed). This is what enables compliance/progress
comparison. It's also the most complex piece, so it's the honest place to defer —
ship with `exercises → sessions → sets → measurements` and add protocols once the
logging habit sticks.

### Wide-nullable columns over EAV on `sets`
Different exercises track different things (band: resistance+reps; hold: duration;
pull-up: load+reps). The "clean" EAV answer turns every read into a pivot and kills
type safety for a *known, small* metric set. Nullable columns + a `metric_type`
hint (drives which inputs the UI renders) is the pragmatic win.

### `measurements` is deliberately tall
`(measured_at, metric, value, side)` — every chartable thing (daily pain, ROM in
three planes, grip strength) is the same shape, so a new tracked metric is a new
row value, never a migration. `side` (left/right) lets you chart injured vs.
uninjured baseline — the standard rehab progress signal.

## AI layer decisions

### No embeddings, no vector store
For a single injury, the entire corpus (diagnosis + a dozen resource summaries + a
few appointment outcomes) fits in the model's context window. RAG-at-scale is the
wrong answer at personal scale. Just concatenate the relevant rows. If the library
ever outgrows the window, the next step is SQLite **FTS5** full-text search to pick
relevant docs — still no external service.

### AI output = editable documents, not chat logs
`ai_documents` stores durable, editable artifacts (a question list you tweak, a
plan you revise), not conversation transcripts. `source_kind`/`source_id` is a
polymorphic *provenance breadcrumb* for navigation only — acceptable because
nothing cascades through it. (Polymorphism for "where did this come from," never
for "what owns this.")

### System-prompt framing
The AI helps the user **prepare for, record, and understand** their care — drafts
questions, organizes research, summarizes what was said. It does **not** diagnose
or prescribe, and always points decisions back to the care team. This is both a
safety stance and what makes the output actually useful.

### Single provider (Anthropic), one code path
No bring-your-own-token abstraction in v1 — one `ai.Client`. (Provider interface
can come later if ever wanted.)

### Two Anthropic API gotchas (verified against current docs)
1. **Do not send `temperature`/`top_p`/`top_k`** — Opus 4.7+ (incl. 4.8) rejects
   non-default values with a 400. Omit the fields entirely.
2. **Handle `stop_reason: "refusal"`** — don't assume `content[0].text` exists.

Headers: `x-api-key`, `anthropic-version: 2023-06-01`, `content-type:
application/json` → POST `https://api.anthropic.com/v1/messages`. Mark the reused
case-file system block with `cache_control: {type: "ephemeral"}` for prompt
caching, since it's sent on every flow. Verify exact model IDs on the models page
before shipping — the strings rev periodically.

## Speech-to-text decision

**Do not build on the Web Speech API** — it's least reliable exactly on Safari/iOS,
the target. Instead: record with **MediaRecorder** (well-supported in a PWA),
upload, transcribe **server-side** with a Whisper-class API, then feed the
transcript to Claude for highlights + tasks. Two stages tracked via
`recordings.transcript_status` (`pending → processing → done → failed`), pollable
from htmx.

Caveats: iOS won't record reliably with the screen locked (keep screen awake; Wake
Lock API helps imperfectly). Oklahoma is one-party-consent so recording your own
visit is legal, but some clinics have policies — a quick "mind if I record this for
my notes?" is the safe habit. Audio is the most sensitive data here: encrypt at
rest, allow deleting audio once a transcript exists.

## Suggested build order

1. Core loop: resources + providers + appointments + AI question generator
2. Recordings + transcription + AI highlights/tasks
3. PT logging satellite
4. PWA shell (manifest + service worker) — last
