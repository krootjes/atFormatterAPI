# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Single-file Go service (`main.go`) that fetches an ICS calendar, filters and classifies events by config rules, and exposes a JSON REST API — designed for Home Assistant. No tests. All logic lives in `main.go`.

## Build & run

```bash
go build ./...
go run main.go
```

```bash
docker compose build
docker compose up
```

On first run with no `config.json`, the app creates a default one and exits. Edit it, then restart.

## Architecture

The pipeline runs per request with a 15-minute in-memory cache (`cachedEvents`, protected by `cacheMutex`):

1. `fetchICSBody()` — HTTP GET the ICS URL from config
2. `fetchAndParseEvents()` — parse with `arran4/golang-ical`, filter by `user_filter` and date window
3. `simplifyEvents()` — apply `ignore_rules`, match against `rules` by priority, resolve times (regex from summary text or fallback to `default_start`/`default_end`), one winner per day
4. Handlers return JSON via `writeJSON()`

Auth is optional: if `api_key` is set in config, `authMiddleware` enforces `X-API-Key` header on all routes.

Config is loaded once at startup from `config.json` in the working directory — inside the container this is `/app`, served via the `data/` volume mount.
