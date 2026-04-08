# atFormatterAPI

A small Go service that fetches an ICS/iCal work calendar, parses and filters events, and exposes a simplified per-day schedule as a REST API — designed for use with Home Assistant.

## How it works

1. Fetches your work calendar via an ICS URL
2. Filters events by name (e.g. your name)
3. Applies ignore rules to discard noise (holidays, tickets, notes)
4. Matches remaining events against configurable rules
5. Picks the highest-priority event per day
6. Returns one normalized schedule entry per day

Results are cached in memory for 15 minutes to avoid hammering the calendar source.

## Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Service health + last calendar fetch status |
| GET | `/api/schedule` | Full simplified schedule |
| GET | `/api/schedule/today` | Today's schedule entry |
| GET | `/api/schedule/tomorrow` | Tomorrow's schedule entry |
| GET | `/api/raw` | Raw parsed events (before simplification) |
| GET | `/api/raw/ics` | Raw ICS body as fetched from the source |

### Example response (`/api/schedule/today`)

```json
{
  "date": "2026-04-07",
  "date_human": "07/04/2026",
  "weekday": "dinsdag",
  "type": "Kantoordag",
  "start": "09:00",
  "end": "17:30",
  "summary": "Kantoordag 09:00-17:30"
}
```

## Deployment

The service is designed to run in Docker only. It always listens on internal port `8080`.

### docker-compose.yml

```yaml
services:
  atformatterapi:
    container_name: atformatterapi
    image: ghcr.io/krootjes/atformatterapi:latest
    ports:
      - "8080:8080"
    restart: unless-stopped
    volumes:
      - ./data:/app
```

Mount `./data` as a writable directory. On first run, the app creates a default `config.json` inside it and exits. Edit the config and restart.

### First run

```bash
mkdir data
docker compose up
# app creates data/config.json and exits
# edit data/config.json
docker compose up
```

## Configuration

`config.json` is auto-generated on first run inside the mounted `data/` directory.

```json
{
  "api_key": "",
  "calendar_url": "https://your-calendar-url.ics",
  "user_filter": "Your Name",
  "days_ahead": 30,
  "weekdays": ["sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"],
  "rules": [
    {
      "match": "Morning support",
      "type": "Ochtenddienst",
      "priority": 100,
      "default_start": "08:00",
      "default_end": "16:30"
    },
    {
      "match": "Internal Issues",
      "type": "Kantoordag",
      "priority": 50,
      "default_start": "09:00",
      "default_end": "17:30"
    }
  ],
  "ignore_rules": [
    { "match": "[Holiday]" },
    { "match": "Topdesk Tickets" }
  ]
}
```

### Config fields

| Field | Description |
|-------|-------------|
| `api_key` | Optional. If set, all requests require header `X-API-Key: <key>` |
| `calendar_url` | Required. ICS URL to fetch |
| `user_filter` | Optional. Only process events whose summary contains this string |
| `days_ahead` | How many days ahead to include (default: 30) |
| `weekdays` | Weekday names used in output (index 0 = Sunday) |
| `rules` | List of classification rules (see below) |
| `ignore_rules` | List of patterns to discard before classification |

### Rules

Each rule matches events by case-insensitive substring. The highest-priority matching rule wins per day.

| Field | Description |
|-------|-------------|
| `match` | Substring to match against the event summary |
| `type` | Display name for this event type |
| `priority` | Higher number wins when multiple events exist on the same day |
| `default_start` | Fallback start time if none found in summary |
| `default_end` | Fallback end time if none found in summary |

Times are extracted from the summary text first (e.g. `14:00-22:00`). If not found, `default_start`/`default_end` are used.

### Ignore rules

Events matching any ignore rule are discarded before classification and never appear in output.

| Field | Description |
|-------|-------------|
| `match` | Substring to match against the event summary |

## Authentication

If `api_key` is set in config, include the key in every request:

```
X-API-Key: your-secret-key
```

Requests without a valid key return `401 Unauthorized`. If `api_key` is empty, the API is unprotected.
