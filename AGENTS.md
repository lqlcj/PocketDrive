# Repository Guidelines

## Project Structure & Module Organization

- `cmd/pocketdrive/` starts the Go server; `internal/` contains feature packages (files, cloud storage, WebDAV, auth, sharing, aria2).
- `web/src/` holds React/TypeScript pages, components, and state; `web/public/` holds static assets.
- Go tests sit beside implementation files. `docker/` contains deployment files; `scripts/` contains installation/development helpers.

## Build, Test, and Development Commands

Use Go 1.26.8+ and Node.js 24 (matching Docker); install ffmpeg for video thumbnails. Run from the repository root:

- `npm --prefix web ci`: install locked dependencies.
- `npm --prefix web run build`: type-check and build `web/dist/`. Run this before Go commands because `web/embed.go` embeds these assets.
- `go test ./...`: run backend tests.
- `go build ./cmd/pocketdrive`: compile the server.
- `./scripts/dev.ps1`: start the backend on port 16688 with development credentials; `go run ./cmd/pocketdrive` uses your environment.
- `npm --prefix web run dev`: start Vite at `http://127.0.0.1:5173` in another terminal.

## Coding Style & Naming Conventions

Format Go with `gofmt` (tabs); use lowercase package names. TypeScript uses four spaces, single quotes, semicolons, and strict checking. Use PascalCase for page/component files and camelCase for functions; retain lowercase names in `components/ui/`. No frontend lint script is configured.

## Testing Guidelines

Use Go's `testing` and `net/http/httptest`; name files `*_test.go` and functions `TestBehavior`. Add regression tests for backend behavior changes. No coverage threshold is configured.

S3 integration tests skip without `PD_S3_ENDPOINT`, `PD_S3_BUCKET`, `PD_S3_KEY`, and `PD_S3_SECRET`. Use a disposable bucket; run `go test ./internal/cloud -run E2E -v`.

Review `web/smoke.mjs` before use: it requires undeclared `playwright-core`/`xlsx` dependencies and local Chrome/login adjustments, and writes fixtures to `data/`.

## Commit & Pull Request Guidelines

History mixes version-only releases (`1.1.1`) with descriptive commits and `fix:`/`docs:` prefixes. Use concise, imperative summaries for changes. PRs should explain behavior, link relevant issues, list validation performed, and include screenshots for UI changes.

## Security & Configuration

Configure through `POCKETDRIVE_*` environment variables. Keep `POCKETDRIVE_DB` outside `POCKETDRIVE_DATA_DIR`. Preserve `os.Root` path confinement. Never commit credentials, runtime data, databases, or generated `web/dist/`.
