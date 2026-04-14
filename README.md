# stasher

A label-driven Docker volume backup manager. Add labels to any container and stasher will archive each of its volumes on its own schedule — no scripting required.

## How it works

stasher runs as a sidecar container with access to the Docker socket. On startup, and then every `CHECK_INTERVAL`, it queries Docker for all running containers that carry the `stasher.enabled=true` label. For each configured volume it registers an independent cron job driven by that volume's `schedule` label.

At backup time, data is read through the **Docker `CopyFromContainer` API**. This means:

- No bind-mount host-path configuration needed — the manager only needs the Docker socket and its own `/backups` volume.
- Works on running *and* stopped containers.
- No privileged mode required.

Each volume produces its own `.tar.gz` archive. Files are stored at their original in-container paths (`var/lib/postgresql/data/…`) so they can be restored with a plain `tar x`. A `backup-manifest.json` is written as the first entry in every archive for easy inspection.

## Quick start

```yaml
# docker-compose.yml
services:
  stasher:
    image: stasher:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - backups:/backups
    restart: unless-stopped

  postgres:
    image: postgres:16-alpine
    volumes:
      - pg_data:/var/lib/postgresql/data
    labels:
      stasher.enabled: "true"
      stasher.volumes.data.path: "/var/lib/postgresql/data"
      stasher.volumes.data.schedule: "0 2 * * *"   # 02:00 every day
      stasher.volumes.data.retention: "168h"        # keep 7 days

volumes:
  pg_data:
  backups:
```

```sh
docker build -t stasher .
docker compose up -d
```

## Configuration

### Manager environment variables

| Variable | Default | Description |
|---|---|---|
| `BACKUP_DEST` | `/backups` | Directory inside the manager container where archives are written. Mount a volume or bind-mount here. |
| `CHECK_INTERVAL` | `60s` | How often to poll Docker for new or removed containers (Go duration, e.g. `30s`, `5m`). |
| `TZ` | `UTC` | Timezone for cron schedule evaluation (e.g. `America/New_York`, `Europe/Paris`). Mounting `/etc/localtime` is not reliable on Windows/WSL — set this variable instead. |

### Container labels

#### Opt-in

| Label | Required | Default | Description |
|---|---|---|---|
| `stasher.enabled` | yes | — | Set to `true` to opt this container in. |

#### Per-volume labels — `stasher.volumes.<id>.*`

Each volume to back up is declared under a unique `<id>` of your choice (e.g. `data`, `wal`, `redis`). Multiple volumes are supported.

| Label | Required | Default | Description |
|---|---|---|---|
| `stasher.volumes.<id>.path` | yes | — | Absolute path inside the container to back up. |
| `stasher.volumes.<id>.schedule` | no | `@daily` | Cron expression for when to back up (see below). Each volume is scheduled independently. |
| `stasher.volumes.<id>.retention` | no | `168h` | Delete archives older than this duration. Set to `0` to keep forever. |

##### Schedule format

Standard 5-field cron expressions (`minute hour day month weekday`) and `@` aliases are both accepted. Fire times are always computed from the current wall clock, so the schedule is stable across manager restarts.

| Value | Fires at |
|---|---|
| `0 3 * * *` | 03:00 every day |
| `0 */6 * * *` | 00:00, 06:00, 12:00, 18:00 every day |
| `30 2 * * 1` | 02:30 every Monday |
| `0 4 1 * *` | 04:00 on the 1st of each month |
| `@daily` / `@midnight` | 00:00 every day |
| `@hourly` | top of every hour |
| `@weekly` | 00:00 every Sunday |
| `@monthly` | 00:00 on the 1st of each month |
| `@yearly` / `@annually` | 00:00 on January 1st |

#### Container-level options — `stasher.options.*`

| Label | Required | Default | Description |
|---|---|---|---|
| `stasher.options.stop_during_backup` | no | `false` | Stop the container before copying and restart it afterwards. Recommended for databases that do not support online backups. |

### Multiple volumes example

```yaml
labels:
  stasher.enabled: "true"
  # Primary data — daily at 03:00, keep 30 days
  stasher.volumes.data.path: "/var/lib/postgresql/data"
  stasher.volumes.data.schedule: "0 3 * * *"
  stasher.volumes.data.retention: "720h"
  # WAL archive — every hour, keep 3 days
  stasher.volumes.wal.path: "/var/lib/postgresql/wal"
  stasher.volumes.wal.schedule: "@hourly"
  stasher.volumes.wal.retention: "72h"
```

Each volume is backed up independently on its own schedule and produces its own set of archives.

## Archive format

Archives are named `<container>-<id>-<timestamp>.tar.gz`:

```
postgres-data-20260410-020000.tar.gz
├── backup-manifest.json          ← metadata; always the first entry
└── var/lib/postgresql/data/
    ├── PG_VERSION
    ├── base/
    └── …
```

Inspect the manifest without extracting the archive:

```sh
tar xOf backups/postgres-data-20260410-020000.tar.gz backup-manifest.json | jq .
```

```json
{
  "container_id": "a1b2c3d4e5f6",
  "container_name": "postgres",
  "timestamp": "2026-04-10T02:00:00Z",
  "path": "/var/lib/postgresql/data"
}
```

## Restore

```sh
# Restore all files to their original paths
tar xzf backups/postgres-data-20260410-020000.tar.gz -C /

# Restore into a specific directory for inspection
tar xzf backups/postgres-data-20260410-020000.tar.gz -C /tmp/restore
```

## Examples

### PostgreSQL

```yaml
labels:
  stasher.enabled: "true"
  stasher.volumes.data.path: "/var/lib/postgresql/data"
  stasher.volumes.data.schedule: "0 2 * * *"   # 02:00 every day
  stasher.volumes.data.retention: "336h"        # 14 days
  # produces: postgres-data-20260410-020000.tar.gz
```

### Redis — stop for RDB consistency

```yaml
labels:
  stasher.enabled: "true"
  stasher.options.stop_during_backup: "true"
  stasher.volumes.data.path: "/data"
  stasher.volumes.data.schedule: "@hourly"
  stasher.volumes.data.retention: "24h"
  # produces: redis-data-20260410-010000.tar.gz
```

### Multiple independent volumes

```yaml
labels:
  stasher.enabled: "true"
  stasher.volumes.db.path: "/var/lib/postgresql/data"
  stasher.volumes.db.schedule: "0 3 * * *"   # 03:00 daily
  stasher.volumes.db.retention: "720h"
  stasher.volumes.wal.path: "/var/lib/postgresql/wal"
  stasher.volumes.wal.schedule: "@hourly"
  stasher.volumes.wal.retention: "72h"
```

## Building

```sh
# Local binary
go build -o stasher .

# Docker image
docker build -t stasher .
```

Requires Go 1.26+ and a reachable Docker daemon.
