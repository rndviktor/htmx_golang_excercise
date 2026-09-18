---
name: cartograph
description: Workflows for the Pg HTMX Admin repo (Go + HTMX + CodeMirror + Chart.js). Use when working on this project — rebuilding static bundles (CodeMirror / chart.js tree-shaking via esbuild), the Docker production build (docker/Dockerfile), the hot-reload dev container (docker/Dockerfile_dev + docker/docker-compose.dev.yml + docker/air.toml), port config (PORT/.env), or verifying the containerized app.
---

# Pg HTMX Admin — dev/build workflows

Go + HTMX app (PostgreSQL admin UI). Entry point `cmd/server/main.go`; `templates/` and `static/` are embedded into the binary via `//go:embed` (`static.go`), so any change there requires a Go rebuild to take effect.

## Frontend bundling (tree shaking)

`scripts/build-editor.mjs` builds two bundles with **esbuild** (`npm run build:editor`):

- `static/codemirror.bundle.js` — CodeMirror 6 + SQL formatter, from `static/js/sql-editor.js`.
- `static/vendor/chart.bundle.js` — **tree-shaken** Chart.js, from `static/js/chart-entry.js` (imports only LineController, LineElement, PointElement, LinearScale, CategoryScale, Legend, Tooltip, Filler; exposes `window.Chart`).

Rules:

- Regenerate bundles after editing `static/js/sql-editor.js`, `static/js/sql-formatter.js`, `static/js/chart-entry.js`, or bumping esbuild/CodeMirror/Chart.js deps.
- Never edit the generated bundles by hand — they live in `static/` and `static/vendor/`.
- The old `static/vendor/chart.umd.min.js` UMD copy is gone; do not reintroduce it.
- Do not add `import` to any classic `static/js/*.js` script (they run as non-module scripts; `window.Chart`/`window.SqlEditor` globals bridge them to the module bundles).
- After rebundling you must rebuild/restart the server (bundle files are embedded) and hard-refresh the browser to bust the cached JS.

## Configuration

- `.env` is loaded by `internal/env/env.go` (`env.Load()` at startup; existing OS env vars win). Missing `.env` is not an error.
- `PORT` (loaded via `env.Get`) — listen address, default `:8080`. Use e.g. `PORT=:3000` (leading colon). Documented in `.env.example`.
- `PGHTMX_ADMIN_DEFAULT_EMAIL` / `PGHTMX_ADMIN_DEFAULT_PASSWORD` — demo credentials, seeded into SQLite (PBKDF2 hashed).
- SQLite metadata DB `pgadmin4.db` is created in the working directory on first run.

## Production Docker build

`docker/Dockerfile` — builds `./cmd/server`, runs as distroless `nonroot` (uid 65532) with `WORKDIR /data` (writable, holds `pgadmin4.db`). Build context must be the repo root:

```sh
docker build -f docker/Dockerfile -t bos-ui:latest .
```

`.dockerignore` keeps the context lean (excludes `node_modules/`, `.git`, `.env`, `pgadmin4.db`, `working_example/`). Persist the SQLite DB with a volume at `/data`.

## Dev container (hot reload)

`docker/docker-compose.dev.yml` lives in `docker/`, so relative paths use `..` for context and the source mount:

- `build.context: ./..`, `dockerfile: docker/Dockerfile_dev` (from the project root).
- Volume `..:/app` mounts the repo; named volumes hold `node_modules` (Linux esbuild, separate from host win32 deps), `/go/pkg/mod`, and `/root/.cache/go-build`.
- `HOST_PORT` env overrides the published host port (default 8080; the user's `pghtmx-container` often occupies 8080).
- `docker/Dockerfile_dev` — golang:alpine + node/npm + Air (`go install github.com/air-verse/air@latest`, pinned automatically).
- `docker/air.toml` — Air watches `go/html/js/mjs/css/mod/sum` files: `pre_cmd` runs `npm run build:editor` (esbuild + tree shaking) then `go build -o /tmp/air/main ./cmd/server`. Excludes the generated bundles and `pgadmin4.db*` from the watch to avoid rebuild loops. Polling enabled for bind-mounts.

Run:

```sh
docker compose -f docker/docker-compose.dev.yml up [--build]
HOST_PORT=18081 docker compose -f docker/docker-compose.dev.yml up  # if 8080 taken
docker compose -f docker/docker-compose.dev.yml down -v              # reset node_modules/go cache volumes
```

The server logs `Server started on http://localhost:8080` (container port; map to HOST_PORT).

**Do NOT verify the whole application is running when the dev container is up** — the user already watches its console output, so do not curl/login/smoke-test or run additional checks of the app itself. Only inspect `docker compose logs` or `docker ps` status when something is actually failing or the user asks. If the container is stopped or not present, a quick smoke-test is fine.

Air config changes are read on container start; restart the container to apply them.

## Verification checklist

- Go compiles: `go build ./cmd/server`.
- After JS/asset edits: run `npm run build:editor`, confirm both bundle sizes printed, then restart server.
- Chart bundle should be ~166 KB (a full `chart.js/auto` bundle is ~200 KB) — if it grows to ~200 KB, tree shaking is broken (check `static/js/chart-entry.js` imports).