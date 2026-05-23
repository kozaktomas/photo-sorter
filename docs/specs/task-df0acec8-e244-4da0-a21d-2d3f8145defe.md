## Context

The local production photo-sorter instance was installed today as a .deb (systemd `photo-sorter.service`, listening on `0.0.0.0:5112`, DB on `localhost:5434` in the `photo-sorter-pgvector` container at `/opt/photo-sorter/`). It is now expected to be a "production" service: backups already run via `photo-sorter-backup.timer` (daily 02:30 → `/mnt/nas-botka/backups/photo-sorter/`). What's still missing for the "production" label is observability + alerting.

The Pi already runs a full Grafana stack:
- **Mimir** on `:9009` (metrics store)
- **Alloy** on `:12345` (scrapes + remote-write to Mimir). Live config at `/opt/mimir/config/alloy.config.alloy`, repo source at `/home/pi/projects/rpi/mimir/config/alloy.config.alloy`. `/opt` copy is the one Alloy reads — keep them in sync.
- **Alertmanager** on `:9093`
- **Grafana** dashboards live in `/opt/mimir/`-ish; existing alerting rules are wherever the rpi project keeps them — search for `*.yaml` containing `alert:` under `/home/pi/projects/rpi/`.

## Deliverables

### 1. `/metrics` endpoint in the Go binary

Photo-sorter currently has **no** Prometheus instrumentation (verified by grep across `internal/web/`, `cmd/serve.go`). Add it:

- Pull in `github.com/prometheus/client_golang/prometheus` and `.../promhttp`.
- Register on the **same** Chi router used by `serve` at path `/metrics` (not under `/api/v1/`). Keep it unauthenticated — the port (5112) is LAN/Tailscale-only and standard Prom convention is no auth.
- Middleware for the HTTP surface (excluding `/metrics` itself):
  - `photo_sorter_http_requests_total{method,route,status}` counter
  - `photo_sorter_http_request_duration_seconds{method,route}` histogram (default buckets)
  - `photo_sorter_http_inflight_requests` gauge
- DB pool gauges from `sql.DB.Stats()`: `open`, `in_use`, `idle`, `wait_count`, `wait_duration_seconds_total`. Refresh on scrape via a collector.
- Embedding service health: a gauge `photo_sorter_embedding_service_up` if `EMBEDDING_URL` is configured (best-effort HEAD/GET on startup + periodic).
- Background-job counters: upload jobs, sort jobs, process jobs, export jobs — completed / failed. Look at `internal/web/handlers/upload_job.go`, `sort.go`, `process.go`, `book_export_job.go` for hooks.
- Wire from `cmd/serve.go` so that `WEB_PORT=5112` exposes `GET /metrics` returning the standard exposition format.

**Test plan**: unit test the middleware bumps counters; integration smoke via `curl localhost:5112/metrics | grep photo_sorter_`.

Then **rebuild the .deb snapshot** (`/tmp/goreleaser-old/goreleaser release --snapshot --clean --skip=publish` or whatever's on PATH; current Pi-local install was via this command) and `sudo dpkg -i dist/photo-sorter_*_linux_arm64.deb`. `sudo systemctl restart photo-sorter`. Verify `/metrics` reachable.

### 2. Alloy scrape job

Edit `/home/pi/projects/rpi/mimir/config/alloy.config.alloy` to add a `prometheus.scrape` block (or extend an existing one) targeting `localhost:5112` with `metrics_path = "/metrics"`. Job label: `photo-sorter`. Instance label: `pi`. Send to the existing Mimir remote_write.

After editing the rpi source, copy to `/opt/mimir/config/alloy.config.alloy` and restart the Alloy container (`cd /opt/mimir && docker compose restart alloy`). Verify via `curl http://localhost:12345/api/v0/web/targets` (or whatever the live targets endpoint is) that the photo-sorter target is `up`.

### 3. Alertmanager rules

Find the existing rules location (probably under rpi/mimir/ or /opt/mimir/). Add rules for:

- **photo-sorter service down** — `absent_over_time(photo_sorter_http_inflight_requests{job="photo-sorter"}[2m])` or equivalent. Severity warning, 5 min for.
- **High 5xx rate** — `sum(rate(photo_sorter_http_requests_total{status=~"5.."}[5m])) by (instance) > 0.1`. Severity warning.
- **DB pool saturated** — `photo_sorter_db_pool_in_use / photo_sorter_db_pool_open > 0.9` for 10 min.
- **Backup hasn't run in 30h** — exporter or `node_exporter` `node_filesystem_size_bytes` won't work for this; instead expose a metric `photo_sorter_last_backup_timestamp_seconds` from the binary itself (it could read the latest `metadata.json` under the backup dir at scrape time), OR write a tiny shell exporter that the timer drops a file the binary reads. **Decide based on what's least intrusive**; document the choice.
- **Disk fill** — `node_filesystem_avail_bytes{mountpoint="/"} / node_filesystem_size_bytes{mountpoint="/"} < 0.1` for 30 min (node_exporter is presumably already scraped — confirm and only add if not duplicated).

Push notifications go through the existing Alertmanager → wherever (Slack / OpenClaw?). The user already has that pipeline; just slot new rules into the existing alertmanager_rules group.

### 4. Documentation

- `CLAUDE.md` — add a short `### Metrics & alerting` subsection under `### Web UI` documenting the `/metrics` endpoint, the env knobs that affect it, and the alert names.
- `docs/architecture.md` — note that Prometheus metrics are exposed (if a metrics doc already exists, link to it; otherwise inline 5–10 lines is fine).

## Out of scope

- Grafana dashboard for photo-sorter (next task — file separately once metrics are flowing).
- Migration of data from `/opt/photoprism-prod` into the production instance (different task — script is staged at `/home/pi/migrate-photoprism.sh`, but currently points at the dev instance and needs repointing).
- Adding auth to `/metrics`. Reconsider if/when the port becomes public.

## Acceptance criteria

- `curl http://localhost:5112/metrics` returns Prometheus text with at least `photo_sorter_*` counters and DB pool gauges.
- Alloy's live targets page shows `photo-sorter` as `up=1`.
- `amtool` (or the Alertmanager UI on `:9093`) lists the new rules and they evaluate (`active=0` is fine while everything is healthy).
- Pre-commit hook (`make lint`, frontend lint) passes.
- One PR to photo-sorter repo, one PR (or push to main if that's the workflow) to rpi repo. Reference each other in the descriptions.