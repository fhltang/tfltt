# CLAUDE.md

## Project Overview

**tfltt** is a Go web application that renders London Underground timetables by calling the TfL (Transport for London) API.

## Architecture

- [main.go](main.go) — HTTP server, two handlers (`/` and `/timetable`), TfL client setup
- [timetable_renderer.go](timetable_renderer.go) — `TimetableRenderer`: transforms TfL timetable API responses into ASCII text tables
- [tfl/client/](tfl/client/) — auto-generated REST client from the TfL OpenAPI spec via `go-swagger`; **do not edit manually**
- [testdata/](testdata/) — JSON fixtures from real TfL API responses, used by unit tests

## Endpoints

| Path | Query params | Description |
|------|-------------|-------------|
| `/` | — | Lists all Tube lines and route segments as a linked HTML table |
| `/timetable` | `line`, `from`, `to` | Renders departure/arrival timetable for a station pair |

## Configuration

API key resolution order:
1. `TFL_APP_KEY` environment variable (used on Cloud Run)
2. `app_key.txt` in the project root (used locally)

Get a key from https://api-portal.tfl.gov.uk/

## Common Commands

```bash
# Run locally
go run .

# Run tests
go test ./...

# Build
go build -o tfltt main.go timetable_renderer.go
```

## Deployment

Google Cloud Run via `gcloud run deploy`:

```bash
gcloud run deploy tfltt \
  --source . \
  --platform managed \
  --region europe-west1 \
  --allow-unauthenticated \
  --set-env-vars TFL_APP_KEY=your_key_here
```

The Dockerfile uses a multi-stage build: Go 1.25 Alpine builder → minimal Alpine runtime. `CGO_ENABLED=0` produces a fully static binary.

## Regenerating the TfL Client

If `tfl_swagger.json` is updated:

```bash
# Fetch latest spec
curl -o tfl_swagger.json https://api.tfl.gov.uk/swagger/docs/v1

# Regenerate (requires go-swagger installed)
swagger generate client -f tfl_swagger.json -t tfl --skip-validation
```

## Key Design Notes

- The generated client in `tfl/client/` is the entire TfL API surface; only `line` endpoints are used in the app.
- Timetable rendering works by adding `TimeToArrival` offsets (from `StationIntervals`) to each journey's base departure time.
- `RenderAsHtml` on `TimetableRenderer` is not yet implemented (returns a placeholder).
- All handlers are stateless — no caching, every request hits the TfL API.
