# ical-formatter-api

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
  "summary": "Kantoordag 09:00-17:30",
  "is_def": true
}
```

The `is_def` field is `true` once the configured trigger (e.g. Friday evening) for that week has passed, indicating the schedule is definitive rather than provisional.

## Deployment

The service is designed to run in Docker only. It always listens on internal port `8080`.

### docker-compose.yml

```yaml
services:
  ical-formatter-api:
    container_name: ical-formatter-api
    image: ghcr.io/krootjes/ical-formatter-api:latest
    ports:
      - "8080:8080"
    restart: unless-stopped
    volumes:
      - ./data:/app
```

Mount `./data` as a writable directory. On first run, the app creates a default `config.json` inside it and exits. Edit the config and restart.

### First run

```bash
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
  "definitive_from": {
    "weekday": "friday",
    "time": "19:00",
    "timezone": "Europe/Amsterdam"
  },
  "rules": [
    {
      "match": "matchA",
      "type": "replaceA",
      "priority": 100,
      "default_start": "08:00",
      "default_end": "16:30"
    },
    {
      "match": "matchB",
      "type": "replaceB",
      "priority": 50,
      "default_start": "09:00",
      "default_end": "17:30"
    }
  ],
  "ignore_rules": [
    { "match": "ignoreC" },
    { "match": "ignoreD" }
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
| `weekdays` | Weekday display names in output (index 0 = Sunday, supports any language) |
| `definitive_from` | Optional. Trigger that marks a week's schedule as definitive (see below) |
| `rules` | List of classification rules (see below) |
| `ignore_rules` | List of patterns to discard before classification |

### `definitive_from`

Configures when a week's schedule becomes definitive. Each Mon–Sun week has exactly one trigger; once the configured weekday + time has passed, all events in that week get `is_def: true`.

| Field | Description |
|-------|-------------|
| `weekday` | Weekday of the trigger (matches against your `weekdays` array or English fallback) |
| `time` | Time of day the trigger fires, e.g. `"19:00"` |
| `timezone` | IANA timezone for the trigger, e.g. `"Europe/Amsterdam"` |

Omit `definitive_from` entirely to always get `is_def: false`.

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

---

## Home Assistant

### REST sensor

Add the following to your `rest.yaml` to fetch the schedule as a sensor. Store your API key in `secrets.yaml`.

**`secrets.yaml`:**
```yaml
ical_formatter_api_key: "your-api-key"
```

**`rest.yaml`:**
```yaml
- resource: http://<your-host>:8080/api/schedule
  timeout: 15
  scan_interval: 3600
  headers:
    X-API-Key: !secret ical_formatter_api_key
  sensor:
    - name: Werkplanning
      unique_id: werkplanning_sensor
      value_template: "{{ value_json['data'] | list | length }} diensten"
      json_attributes:
        - data
```

To manually refresh the sensor without waiting for `scan_interval`, go to **Developer Tools → Actions**, call `homeassistant.update_entity` and select `sensor.werkplanning`.

### Dashboard card

Requires the [html-template-card](https://github.com/PiotrMachowski/Home-Assistant-Lovelace-HTML-Jinja2-Template-card) custom card.

Entries with `is_def: true` are highlighted green (definitive), others yellow (provisional). The `is_def` value comes directly from the API — no date logic in the frontend.

```yaml
type: custom:html-template-card
title: Werkplanning Daan
ignore_line_breaks: true
content: >
  {% set items = state_attr('sensor.werkplanning', 'data') or [] %}
  <div style="margin-bottom:10px;display:flex;gap:16px;font-size:0.85em;opacity:0.8;">
    <div style="display:flex;align-items:center;gap:6px;">
      <div style="width:12px;height:12px;border-radius:3px;background:rgba(0,200,100,0.4);"></div>
      <span>Definitief</span>
    </div>
    <div style="display:flex;align-items:center;gap:6px;">
      <div style="width:12px;height:12px;border-radius:3px;background:rgba(255,200,0,0.4);"></div>
      <span>Onder voorbehoud</span>
    </div>
  </div>
  <table style="width:100%;border-collapse:collapse;text-align:left;">
    <thead>
      <tr style="border-bottom:1px solid rgba(255,255,255,0.15);">
        <th style="padding:6px 4px;">Datum</th>
        <th style="padding:6px 4px;">Dag</th>
        <th style="padding:6px 4px;">Type</th>
        <th style="padding:6px 4px;">Tijd</th>
      </tr>
    </thead>
    <tbody>
      {% for i in items %}
        {% if i.is_def %}
          {% set bg = 'background:rgba(0,200,100,0.15);' %}
        {% else %}
          {% set bg = 'background:rgba(255,200,0,0.15);' %}
        {% endif %}
        <tr style="border-bottom:1px solid rgba(255,255,255,0.08);{{ bg }}">
          <td style="padding:6px 4px;">{{ i.date_human[:5] }}</td>
          <td style="padding:6px 4px;">{{ i.weekday[:2] | capitalize }}</td>
          <td style="padding:6px 4px;">{{ i.type }}</td>
          <td style="padding:6px 4px;">{{ i.start }} – {{ i.end }}</td>
        </tr>
      {% endfor %}
      {% if items|length == 0 %}
        <tr>
          <td colspan="4" style="padding:6px 4px;opacity:.7;"><i>Geen diensten gevonden.</i></td>
        </tr>
      {% endif %}
    </tbody>
  </table>
```
