# syslog-flow

Plaintext syslog collection with a small web UI.

`syslog-flow` runs two processes in one container:

- `rsyslogd` listens for syslog on UDP and TCP `514`
- `syslog-flow` serves the web UI and stats API on `2200`

## What It Does

- Stores logs as plain text under `/logs` with date-based folders and one file per device
- Shows a live log view in the browser
- Supports day views, per-file views, global search, and severity filters
- Shows statistics and device activity
- Exposes a simple numeric stats API at `GET /api/stats`

Example log layout:

```text
logs/
  2026/
    05/
      01/
        router.log
        switch.log
```

Stored lines include an internal ingest timestamp prefix:

```text
2026-05-03T09:14:22-05:00 | 2026-05-03T09:14:21-05:00 router info dnsmasq: started
```

The UI hides that prefix when rendering logs, but uses it for ordering, recent activity, and live updates.

## Build

Build the container image:

```sh
docker build -t syslog-flow .
```

Build the Go binary directly:

```sh
go build ./cmd/syslog-flow
```

Direct host execution is mainly for development. The binary expects the same absolute paths the container uses:

- `/logs`
- `/config`
- `/resources`

## Install / Run

The intended deployment is the included Compose setup:

```sh
docker compose up --build -d
```

Published ports:

- `2200/tcp`: web UI and stats API
- `514/udp`: syslog ingest
- `514/tcp`: syslog ingest

Default UI URL:

```text
http://localhost:2200
```

Point devices at this host on syslog UDP/TCP port `514`.

## Config

The container mounts `./config` to `/config`:

```yaml
volumes:
  - ./config:/config
```

On a fresh install, `syslog-flow` creates missing config files with defaults before startup. The `config/` directory is intentionally ignored by git so local settings stay local.

Generated files:

- `config/app.json`
- `config/device-colors.json`
- `config/interface-colors.json`
- `config/status-colors.json`

### app.json

Controls browser polling intervals:

```json
{
  "live_refresh_seconds": 2,
  "stats_refresh_seconds": 10,
  "overview_refresh_seconds": 10
}
```

### device-colors.json

Optional per-device heading colors in the UI:

```json
{
  "router": "#00B4FF",
  "switch": "#EB8C00"
}
```

### interface-colors.json

Interface theme colors for light and dark mode:

```json
{
  "light": {
    "accent": "#0078ff",
    "line": "rgba(0, 0, 0, 0.18)"
  },
  "dark": {
    "accent": "#0078ff",
    "line": "#2c3b36"
  }
}
```

This file defines the main interface palette used by the UI. Missing keys fall back to built-in defaults.

### status-colors.json

Optional severity colors for rendered log tags:

```json
{
  "emerg": "#FF4D4D",
  "alert": "#FF4D4D",
  "crit": "#FF4D4D",
  "err": "#FF6B6B",
  "warning": "#FFD166",
  "notice": "#7BDFF2",
  "info": "#9AA89F",
  "debug": "#8E9AAF"
}
```

## Web UI

Current UI behavior:

- `/`: live tail across recent logs
- `/statistics`: summary view with device activity and totals
- `/day/YYYY/MM/DD`: logs for a specific day
- `file=...`: limit a day view to one device file
- `q=...`: text filter
- `level=...`: structured severity filter for `emerg`, `alert`, `crit`, `err`, `warning`, `notice`, `info`, `debug`

The viewer supports:

- live refresh while scrolled to the bottom
- loading older lines when scrolling upward in day views
- global search across stored logs
- mobile `Jump to Top` and `Jump to Bottom` controls for long log views

## API

`GET /api/stats`

Example response:

```json
{
  "critical_5m": 0,
  "today_lines": 48060,
  "all_lines": 48060,
  "lines_per_second": 0,
  "log_bytes": 4823119,
  "log_days": 1,
  "devices": 10
}
```

This endpoint returns raw numeric values and is suitable for simple external polling.

## Notes

- Logs are stored as plain text only. There is no database.
- `entrypoint.sh` keeps `rsyslogd` and the web process running together and stops the container if either one exits.
