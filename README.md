<p align="center"><img src="resources/apple-touch-icon.png"></p>
<h1 align="center">syslog-flow</h1>

<p align="center"><b>A lightweight syslog server with plain-text storage and a clean, responsive web UI.</b>
</p>

<p align="center"> Send syslogs from your routers, switches, firewalls, NASes, or Linux servers and watch them live in your browser, or browse historical logs from disk using the intuitive sidebar. Plain-text logs. No database, Grafana, Kubernetes, or Elasticsearch.
</p>

![banner](banner.png)

I had always noticed "Remote Syslog" options on various devices. When I learned about how it worked, I was looking forward to setting it up, as it makes troubleshooting failures and real time visibility much easier. However, when I explored the available options, I was shocked to see how overly complicated they were. I just wanted somewhere to send and store logs, with a clean interface to read them. So I created syslog-flow - a Docker container that is simple and self contained.

<details>
<summary>Actual repository size (without marketing assets)</summary>

```text
4	syslog-flow/.github/workflows/docker-build.yml
4	syslog-flow/Dockerfile
4	syslog-flow/cmd/syslog-flow/cache_test.go
4	syslog-flow/cmd/syslog-flow/day_deletion.go
4	syslog-flow/cmd/syslog-flow/index_test.go
4	syslog-flow/cmd/syslog-flow/log_context.go
4	syslog-flow/cmd/syslog-flow/logviewer.go
4	syslog-flow/cmd/syslog-flow/pruning_settings.go
4	syslog-flow/cmd/syslog-flow/settings_test.go
4	syslog-flow/cmd/syslog-flow/web_test.go
4	syslog-flow/docker-compose.yml
4	syslog-flow/entrypoint.sh
4	syslog-flow/go.mod
4	syslog-flow/rsyslog.conf
8	syslog-flow/.github/workflows
8	syslog-flow/cmd/syslog-flow/actions_reader.go
8	syslog-flow/cmd/syslog-flow/cache.go
8	syslog-flow/cmd/syslog-flow/config_generator.go
8	syslog-flow/cmd/syslog-flow/day_deletion_test.go
8	syslog-flow/cmd/syslog-flow/device_colors_settings.go
8	syslog-flow/cmd/syslog-flow/device_colors_settings_test.go
8	syslog-flow/cmd/syslog-flow/log_context_test.go
8	syslog-flow/cmd/syslog-flow/main_test.go
8	syslog-flow/cmd/syslog-flow/pruning.go
8	syslog-flow/cmd/syslog-flow/search.go
8	syslog-flow/go.sum
12	syslog-flow/.github
12	syslog-flow/cmd/syslog-flow/pruning_test.go
12	syslog-flow/cmd/syslog-flow/search_test.go
12	syslog-flow/cmd/syslog-flow/testdata/viewer_test.cjs
16	syslog-flow/cmd/syslog-flow/actions.go
16	syslog-flow/cmd/syslog-flow/testdata
20	syslog-flow/cmd/syslog-flow/index.go
24	syslog-flow/cmd/syslog-flow/settings.go
28	syslog-flow/cmd/syslog-flow/actions_settings.go
28	syslog-flow/cmd/syslog-flow/actions_test.go
36	syslog-flow/cmd/syslog-flow/main.go
60	syslog-flow/cmd/syslog-flow/web.go
368	syslog-flow/cmd/syslog-flow
372	syslog-flow/cmd
416	syslog-flow
```

</details>

The `syslog-flow` container runs two processes:

- `rsyslogd` listens for syslog on UDP and TCP `514`
- `syslog-flow` serves the web UI and stats API on `2200`

![banner](banner-2.png)

----

## What It Does

- Ingests logs using rsyslog for reliable parsing
- Shows a live-updating log view in the browser
- Stores ingested logs as plain text under `./logs` with logical folder organization.
- Supports day views, per-device views, global search, and severity filters
- Search opens a popup with optional text and inclusive From / Through date filters, defaulting to seven days ago through today. Reopening it from results preserves your selected dates. Global results use the normal log viewer and support device and severity filters while preserving the selected dates. Blank dates include all days; text is optional. Search keeps the newest 5,000 matching entries.
- Click a timestamp in global results to open its original day/device file, load surrounding entries without search filters, and highlight the selected entry. This context view pauses live log updates and offers **View latest logs** to return to the live view; scrolling up still loads older entries. Links use the physical line number plus a checksum, so repeated timestamps are supported and changed or deleted source entries show an unavailable message.
- Shows simple statistics and live device activity at '/statistics`
- Exposes a simple numeric stats API at `GET /api/stats`
- Caches key information in per-day JSON files for super fast startup and responsive API

Example log layout on-disk:

```text
logs/
  2026/
    05/
      01/
        router.log
        nas.log
        homeassistant.log
        2026-05-01.json
```

## Install (Recommended: Published Docker Image)

Find a good, logical location for the log storage, then create a folder for your Compose file, configuration, and logs:

```shell
mkdir -p syslog-flow
cd syslog-flow
```

Save the following as `docker-compose.yml` in that folder:

```yaml
services:
  syslog-flow:
    image: ghcr.io/inventor7777/syslog-flow:latest
    container_name: syslog-flow
    restart: unless-stopped
    ports:
      - "2200:2200"
      - "514:514/udp"
      - "514:514/tcp"
    volumes:
      - ./logs:/logs
      - ./config:/config
      - /etc/localtime:/etc/localtime:ro
```

Start the container:

```shell
docker compose up -d
```

Then, start right away by pointing devices at the server IP on syslog UDP/TCP port `514`, and then navigate to `<SERVER-IP:2200>` to view the logs in real time.

Default ports:

- `2200/tcp`: **web UI** and stats API 
- `514/udp`: syslog ingest
- `514/tcp`: syslog ingest

## Build from Source (Optional)

Use this option to run local changes or build the image yourself. The repository's `docker-compose.yml` uses `build: .` for this workflow.

Local builds use Docker BuildKit and target your local platform without extra flags. Multi-platform builds use Go cross-compilation for the Linux AMD64 and ARM64 binaries, keeping the Go compiler on the builder's native platform. The publishing workflow still uses QEMU for Debian package installation on the other architecture.

```shell
git clone https://github.com/inventor7777/syslog-flow
cd syslog-flow
docker compose up --build -d
```

To update a source-built installation, first back up your configuration, then cd back into the `syslog-flow` folder and run:

```shell
git pull
docker compose up --build -d
```

----

## Roadmap
- Reverse proxy and Home Assistant Ingress support
- Packaged as Home Assistant app


## Notes

- Logs are stored as plain text only. There is no database.
- Live day views keep up to 5,000 rows during automatic updates. Scroll up to load older entries from disk.
- Device and severity pill lists compact into scrollable rows when the log viewer would otherwise have less than 325px of height, and expand again when space allows. This responds to the actual viewer space rather than a fixed screen-height breakpoint.
- Open log and statistics pages reload on their next successful poll after a server restart or midnight index reset. The URL and filters stay selected; scroll position and loaded history reset.
- Completed log days keep a small JSON summary beside their `.log` files, such as `logs/2026/05/05/2026-05-05.json`. Missing or unusable summaries are rebuilt on startup; valid existing summaries are only refreshed from Settings.
- `entrypoint.sh` keeps `rsyslogd` and the web process running together and stops the container if either one exits.
- Startup leaves existing log permissions unchanged and does not require POSIX ACL support. On NAS storage, allow the container to read and write the mounted log and configuration folders through the dataset's permissions.
- Basic mobile support is available! To keep it light, I did not implement features like collapsible sidebar, but I did test on iPhones of various sizes and added Jump to Top/Bottom buttons, among other things.
- PRs, issues, and discussions are welcome! However, please keep in mind that this exists to be extremely lightweight. If you need advanced features *(e.g Kubernetes, multi-user support, Grafana)*, feel free to fork the repo and develop a custom version.
- Each log on disk will have an extra timestamp of when the server receives the log. This mitigates issues with devices that report strange dates on boot.
- The Compose file follows the server timezone via `/etc/localtime` (used for day folders and day-based stats)
- syslog-flow is *not* a SIEM. It just accepts, stores, and views logs.
- Why not use a DB? Because there are other solutions for that, and I dislike gray box software which uses a proprietary or hard-to-access DB schema. The logs here are simply text files, and you can move them around freely without breaking anything. Of course, you will want to refresh the JSON caches if you do that :)
- Disclaimer: all Go code was written by GPT 6.1 Sol, GPT 5.6 Terra, and 5.4 Codex. However, I did use common sense and I tested everything, as I use this myself.


## Details, Config, and API

Example config for a client rsyslog (`/etc/rsyslog.d/60-syslog-flow.conf`:

```rsyslog
*.* @192.168.x.x:514
```

---

On startup, `syslog-flow` creates missing config files and also fills in missing top-level keys in `app.json` and `status-colors.json`.

Generated files:

- `config/app.json`
- `config/device-colors.json`
- `config/interface-colors.json`
- `config/status-colors.json`
- `config/actions.json`
- `config/pruning.json`

The web UI also includes **Settings**, where these existing JSON files can be edited directly. Its **JSON Caches** section shows cache coverage by month and can refresh a completed month in the background.

### app.json

Controls browser polling intervals and the per-file stats tail cache size:

```json
{
  "live_refresh_seconds": 2,
  "stats_refresh_seconds": 5,
  "overview_refresh_seconds": 5,
  "stats_tail_lines": 2000,
  "stats_tail_max_age_hours": 24,
  "top_days_count": 5,
  "homepage_lines": 300
}
```

- `live_refresh_seconds`: how often the live log viewer polls for new lines
- `stats_refresh_seconds`: how often top-bar stats refresh, and how long stats snapshots stay cached
- `overview_refresh_seconds`: how often the `Statistics` page refreshes its totals and device list
- `stats_tail_lines`: how many recent lines per file the stats path may retain in memory
- `stats_tail_max_age_hours`: only files newer than this keep cached tail lines; older files keep cached metadata only
- `top_days_count`: how many busiest days the Statistics page shows
- `homepage_lines`: how many recent lines the home page shows

### device-colors.json

Optional per-device heading colors in the UI:

Use **Settings → Device Colors** to add exact-name or contains rules with a color picker or hex code. Rules can be removed, and contains rules can be moved up or down to change their priority. Save applies the whole list; Reset to Defaults clears all rules. Existing structured and legacy flat JSON configurations are loaded into the form, and saving writes the structured format below.

```json
{
  "exact": {
    "OPNsense": "#00B4FF",
    "TrueNAS": "#EB8C00"
  },
  "contains": [
    {
      "match": "ZenWiFi",
      "color": "#FF4D4D"
    }
  ]
}
```

`exact` is checked first. Then `contains` rules are checked top-to-bottom, and the first match wins. Assigned colors also apply to device pill text on day views and global search results.

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
  "emerg": "#FF0000",
  "alert": "#FF1400",
  "crit": "#FF5A00",
  "err": "#FF9100",
  "warning": "#FFD166",
  "notice": "#7BDFF2",
  "info": "#A4AFA9",
  "debug": "#747C8C"
}
```

## Pruning

**Settings → Pruning** provides an enabled switch and optional age and size limits. Automatic pruning is **off by default**. Set either limit or both, then enable and save. Saving enabled settings can immediately delete existing logs.

- **Maximum age:** number of calendar days to keep, including today. For example, 7 keeps today and the previous six days, using the server timezone.
- **Maximum total size:** size of regular files in dated log folders, including JSON summaries, in MiB (1,024 MiB = 1 GiB). This measures file lengths, not filesystem free space or allocated blocks.
- Leave a limit blank to disable that limit. At least one limit is required when pruning is enabled.

Checks run on startup, after saving settings, and every five minutes, independently of browsers. If either limit is exceeded, entire day folders are permanently deleted, oldest first, until both limits are satisfied. Today's folder and future-dated folders are always protected; if those alone exceed the size limit, usage remains above the limit. Day summaries are deleted with their logs. Deletions and errors are logged by the server; errors also appear in Pruning settings. Invalid configuration stops pruning instead of using older rules.

`config/pruning.json` is created with these defaults; zero means no limit:

```json
{
  "enabled": false,
  "max_age_days": 0,
  "max_size_mib": 0
}
```

## Actions

Open **Settings → Actions** to manage rules. **Add action** and **Edit** open a compact popup with separate Delivery, Match log entries, and Cooldown sections. Enabled switches are available on each rule card and in the popup footer. The X or Escape closes the editor without saving; clicking or releasing a drag outside leaves it open. The only trigger is a new log entry. Select one or more severity checkboxes, optionally set a device filter and a case-insensitive text condition, and choose a delivery method. All conditions must match; any selected severity can trigger the rule. The text condition checks the message and program tag, excluding the timestamp and device name.

- **Device filter:** choose **Include** to match only listed devices, or **Exclude** to match every device except those listed. Type directly in the device list for suggestions from existing devices below the box. Choosing a suggestion completes the current line; devices already listed are hidden from suggestions. You can also enter one exact, case-sensitive device name per line (commas are not accepted); an empty list matches any device in either mode. In `actions.json`, rules use `"device_mode": "include"` or `"exclude"` and a `"devices"` array, such as `["router", "nas"]`. Existing single-`device` rules load as one-item Include lists and use the new format when saved from Settings.
- **Message filter:** choose **Include** to match messages containing any listed phrase, or **Exclude** to reject messages containing any listed phrase. Enter one phrase per line; matching ignores case, blank lines are ignored, and commas remain literal text. An empty list matches any message in either mode. Device and severity conditions must still match. In `actions.json`, `contains_mode` is `"include"` or `"exclude"`, and `contains` remains a newline-separated string (for example, `"contains": "disk unavailable\nnetwork unavailable"`). Existing rules default to Include.
- **Shoutrrr:** enter a service URL supported by [nicholas-fedor/shoutrrr](https://github.com/nicholas-fedor/shoutrrr). 
- **Webhook:** enter an HTTP/HTTPS URL and optional headers as `Name: value`, one per line. syslog-flow sends an HTTP POST with Content-Type application/json by default. Only HTTP 2xx responses count as success; redirects are not followed.
- **Payload:** choose **Log event** for the existing event JSON (webhook) or formatted notification text (Shoutrrr), or **Custom payload** to replace it. Webhooks accept valid JSON or an empty body; Shoutrrr accepts nonempty message text. Custom content is sent literally, with no log fields added and no variable substitution. The Shoutrrr notification title still uses the action name. Payloads are limited to 64 KiB. Existing rules continue using the log event until changed.
- The default cooldown is **300 seconds per rule and device**. A value of 0 sends every match. Cooldowns begin when a notification is queued, including attempts that later fail. Saving rule changes resets cooldowns and cancels queued deliveries; a send already in progress may finish.
- **Test action** uses the saved destination, even for a disabled rule, without changing its cooldown. Log-event mode sends a sample event with `test: true` in webhook JSON. Custom mode sends the configured content exactly as saved, so testing an HA service call can execute the real action. The card shows pending deliveries, the last result, and any queue drops. Results and cooldowns are held in memory.

Actions check today's and yesterday's files once per second, and only appended complete lines are processed. Existing entries are skipped at startup, old days are not replayed, and file positions survive midnight. With no enabled rules, the reader only advances its file positions. File checks and action matching do not hold the viewer's index lock. Lines exceeding 1 MiB are skipped and reported; subsequent lines continue to be processed. Truncation-based rotation (such as copytruncate) is unsupported.

Rules are saved in `config/actions.json`, created with an empty `rules` array. 

To call a Home Assistant action directly, select **Webhook** and configure (example):

- **Destination URL:** `http://192.168.50.x/api/services/light/turn_on`
- **Custom headers:** `Authorization: Bearer YOUR_LONG_LIVED_ACCESS_TOKEN`
- **Payload:** Custom payload
- **JSON request body:** `{"entity_id":"light.kitchen_pendants"}`

The request contains only that custom body; syslog event fields are omitted. This also applies when using Test action.

In `config/actions.json`, the optional `payload` string stores the custom content. Omitting it (or setting it to null) uses the log event; `"payload": ""` sends an empty webhook body.

Example webhook body in Log event mode:

```json
{
  "rule": "Firewall errors",
  "device": "OPNsense",
  "severity": "crit",
  "timestamp": "2026-10-20T12:34:56Z",
  "reported_at": "2026-10-20T12:34:55Z",
  "message": "kernel: disk unavailable",
  "raw": "2026-10-20T12:34:55Z OPNsense crit kernel: disk unavailable",
  "test": false
}
```

`timestamp` is the server's receive time; `reported_at` is the timestamp reported by the device. Shoutrrr notifications contain the rule name, device, severity, receive time, and message.

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

Home Assistant `configuration.yaml` REST config:

```yaml
rest:
  - resource: http://<YOUR-SERVER-IP>:2200/api/stats
    scan_interval: 30
    sensor:
      - name: "Syslog Lines Per Second"
        unique_id: lines_per_second
        value_template: "{{ value_json.lines_per_second }}"
        unit_of_measurement: "lines/s"
        icon: mdi:speedometer
        state_class: measurement

      - name: "Syslog Lines Today"
        unique_id: today_lines
        value_template: "{{ value_json.today_lines }}"
        unit_of_measurement: "lines"
        icon: mdi:format-list-bulleted
        state_class: total_increasing

      - name: "Syslog Lines All Time"
        unique_id: all_lines
        value_template: "{{ value_json.all_lines }}"
        unit_of_measurement: "lines"
        icon: mdi:format-list-bulleted
        state_class: total_increasing

      - name: "Syslog Critical (5m)"
        unique_id: critical_5m
        value_template: "{{ value_json.critical_5m }}"
        unit_of_measurement: "alerts"
        icon: mdi:alert-plus-outline
        state_class: measurement

      - name: "Syslog Log Days"
        unique_id: log_days
        value_template: "{{ value_json.log_days }}"
        unit_of_measurement: "days"
        icon: mdi:calendar-text-outline
        state_class: measurement

      - name: "Syslog Devices"
        unique_id: devices_count
        value_template: "{{ value_json.devices }}"
        unit_of_measurement: "devices"
        icon: mdi:lan
        state_class: measurement
        
      - name: "Syslog Total Size"
        unique_id: log_bytes
        value_template: "{{ value_json.log_bytes }}"
        unit_of_measurement: "B"
        icon: mdi:folder-text-outline
        state_class: measurement
        device_class: data_size
```
