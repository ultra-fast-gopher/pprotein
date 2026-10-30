# pprotein HTTP API

This document describes the HTTP endpoints implemented by this checkout of pprotein.

- Base source revision: `fd0b0b1da93994dc4abb986222b92606c4fe9b2b`
- Main server: `PORT` environment variable, default `9000`
- Agent server: `PORT` environment variable, default `19000`
- All `/api/*` responses set `Cache-Control: no-store`.
- The source has no authentication middleware on these routes. Restrict network access or add authentication at a trusted reverse proxy.

The main server serves the web UI at `/` and the JSON, streaming, and profile UI endpoints below. It also mounts the debug routes described later. The agent binary exposes the same debug routes on its own port.

## Main API summary

`{id}` is the exact snapshot ID returned in `Entry.Snapshot.ID`. The route `/data/latest` returns the latest successful stored body and accepts an optional `label` query parameter.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/event` | Server-sent events for snapshot status changes |
| `GET` | `/api/group/targets` | Read the collection target list |
| `POST` | `/api/group/targets` | Replace and persist the collection target list |
| `GET` | `/api/group/collect` | Start collection for every configured target |
| `GET` | `/api/runs` | List persisted start/stop sessions, newest first |
| `POST` | `/api/runs` | Start a session for the configured pprof and log targets |
| `GET` | `/api/runs/{id}` | Read one session and per-target state |
| `POST` | `/api/runs/{id}/stop` | End a running session and import captured artifacts |
| `GET` | `/api/pprof` | List pprof snapshots and their processing states |
| `POST` | `/api/pprof` | Start one pprof collection |
| `GET` | `/api/pprof/data/{id}` | Download a stored raw pprof body |
| `GET` | `/api/pprof/data/latest` | Download the latest successful raw pprof body |
| `GET` | `/api/pprof/{id}/...` | Google pprof web UI for one stored profile; `/flamegraph` is used by the bundled UI |
| `GET` | `/api/httplog` | List HTTP log snapshots and their processing states |
| `POST` | `/api/httplog` | Start one HTTP log collection |
| `GET` | `/api/httplog/{id}` | Get the processed ALP report as TSV |
| `GET` | `/api/httplog/data/{id}` | Download the stored raw HTTP log body |
| `GET` | `/api/httplog/data/latest` | Download the latest successful raw HTTP log body |
| `GET` | `/api/httplog/config` | Read the ALP YAML configuration |
| `POST` | `/api/httplog/config` | Validate as YAML and replace the ALP configuration |
| `GET` | `/api/slowlog` | List slow query log snapshots and their processing states |
| `POST` | `/api/slowlog` | Start one slow query log collection |
| `GET` | `/api/slowlog/{id}` | Get the processed SLP report as TSV |
| `GET` | `/api/slowlog/data/{id}` | Download the stored raw slow log body |
| `GET` | `/api/slowlog/data/latest` | Download the latest successful raw slow log body |
| `GET` | `/api/slowlog/config` | Read the SLP YAML configuration |
| `POST` | `/api/slowlog/config` | Validate as YAML and replace the SLP configuration |
| `GET` | `/api/memo` | List memo snapshots, including memo text in `Message` |
| `POST` | `/api/memo` | Create a memo snapshot |
| `GET` | `/api/memo/{id}` | Get one memo as JSON |

The API is mounted under the main server origin. For example, if pprotein listens on port 9000, the base URL is `http://pprotein-host:9000`.

## Snapshot data model

List endpoints return a JSON array of `Entry` objects. The Go implementation uses exported field names without JSON tags, so the serialized property names are capitalized as shown here.

```json
{
  "Snapshot": {
    "Type": "pprof",
    "ID": "dcxj4tr4mlh4-pprof.pb.gz",
    "Datetime": "2025-09-20T09:45:09.11223148Z",
    "Repository": {
      "Ref": "refs/heads/main",
      "Hash": "...",
      "Author": "...",
      "Message": "...",
      "Remote": "..."
    },
    "GroupId": "2025-09-20_09-45-09.111339",
    "Label": "app-1",
    "URL": "http://app-1:6060/debug/pprof/profile",
    "Duration": 30
  },
  "Status": "ok",
  "Message": "Ready"
}
```

For HTTP collection entries, `Repository` is parsed from the source endpoint's `X-Git-Repository` response header; if that header is missing or invalid, the current code leaves an empty object. Manually added memo entries have `Repository: null`. Status values are `pending`, `ok`, and `fail`. `Message` reports the current operation (`Collecting`, `Processing`, or `Ready`) or an error message. List order is unspecified.

The `GroupId` links entries created by a single group collection. Direct collection requests can supply their own `GroupId`, or omit it.

## Start/stop sessions

`POST /api/runs` starts every compatible target in the saved `/api/group/targets` configuration. It accepts no request body and returns `202 Accepted` with a `Run` object. The API permits one active session at a time. `GET /api/runs` and `GET /api/runs/{id}` return its persisted state. `POST /api/runs/{id}/stop` returns `202 Accepted` while stop and processing run, then the state becomes `completed`, `partial_failure`, or `failed`.

```json
{
  "ID": "run-18b...",
  "Status": "running",
  "StartedAt": "2026-10-30T02:00:00Z",
  "StopRequestedAt": null,
  "FinishedAt": null,
  "Targets": [
    {
      "Type": "pprof",
      "Label": "app-1",
      "URL": "http://10.0.0.11:6060/debug/pprof/profile",
      "ControlURL": "",
      "Status": "running",
      "Error": "",
      "StartedAt": "2026-10-30T02:00:00Z",
      "SnapshotID": ""
    }
  ]
}
```

Per-target statuses are `starting`, `running`, `completed`, and `failed`. A successful stop imports the captured bytes through the existing pprof, ALP, or SLP processor. The resulting snapshot has `GroupId` equal to the run ID and appears in the existing group view. If pprotein restarts during an active run, the persisted run is marked `interrupted`; `POST /api/runs/{id}/stop` can attempt to recover still-active target captures.

Only `pprof`, `httplog`, and `slowlog` targets participate in sessions. Existing fixed-duration `GET /api/group/collect` behavior remains available. Session start ignores each target's configured `Duration`; the saved field remains required for fixed-duration collection.

### Target-side session endpoints

The Go application or pprotein-agent that owns each resource must expose:

| Method | Path | Request | Response |
| --- | --- | --- | --- |
| `POST` | `/debug/capture/start` | `{"SessionID":"run-id","Type":"pprof"}` (or `httplog` / `slowlog`) | `202 Accepted`; repeated start for the same ID/type returns `200 OK` |
| `POST` | `/debug/capture/stop` | Same session ID and type | `200 OK` with captured bytes as `application/octet-stream`; `204` if no bytes were captured |

If `ControlURL` is empty, pprotein derives the control base URL from the target URL by replacing its `/debug/...` path with `/debug/capture`. To use a different host/path, add `ControlURL` to a target; it is the base URL without the final `/start` or `/stop`, for example:

```json
{
  "Type": "slowlog",
  "Label": "mysql-1",
  "URL": "http://10.0.0.12:19000/debug/log/slowlog",
  "ControlURL": "http://10.0.0.12:19000/debug/capture",
  "Duration": 30
}
```

The pprof capture starts `runtime/pprof` CPU profiling in the process hosting the integration handler. It therefore needs to be integrated into the Go application being measured; a separate pprotein-agent profiles its own process. Only one CPU profile can run in a Go process at once. Log sessions record the file offset at start and return bytes appended by stop; a single log rotation during the session is handled by including the new active file.

Set `PPROTEIN_CONTROL_TOKEN` to the same value on pprotein and target processes to require `Authorization: Bearer <token>` on the target-side start/stop endpoints. If unset, those endpoints are unauthenticated, as are the existing pprotein APIs; restrict access to a trusted network or protect pprotein at a reverse proxy.

## Starting collection

### Direct collection

Send a JSON `SnapshotTarget` to the endpoint for its type:

```json
{
  "GroupId": "benchmark-2026-10-30",
  "Label": "app-1",
  "URL": "http://10.0.0.11:6060/debug/pprof/profile",
  "Duration": 30
}
```

```sh
curl -X POST -H 'Content-Type: application/json' \
  --data-binary @target.json \
  http://pprotein-host:9000/api/pprof
```

Use `/api/httplog` or `/api/slowlog` for the other collection types. `GroupId`, `Label`, and `URL` are strings; `Duration` is an integer number of seconds. The target URL must be reachable from the pprotein server. For HTTP log and slow log targets, the URL normally points at a pprotein debug endpoint on the machine holding the log. The collector sends a GET, appends `?seconds={Duration}`, and advertises `Accept-Encoding: gzip`. Configure the URL without a query string; this revision appends `?seconds=` directly instead of merging query parameters. No custom request headers can be configured for targets.

The POST returns `200 OK` with an empty body after starting the work in the background. It does not mean the remote fetch or analysis succeeded. Poll the list endpoint or subscribe to `/api/event` to observe the final status. Collection errors are reported in the entry's `Message`.

### Group collection

`GET /api/group/collect` reads the saved target list, generates one `GroupId`, and starts requests for all targets concurrently. It returns `200 OK` with no body after the individual collection requests have been accepted. A later target fetch or processor failure appears in that snapshot's status, not necessarily in the group endpoint's response.

### Target list

`GET /api/group/targets` returns a JSON array. `POST /api/group/targets` replaces the full list; it does not append entries.

```json
[
  {
    "Type": "pprof",
    "Label": "app-1",
    "URL": "http://10.0.0.11:6060/debug/pprof/profile",
    "Duration": 30
  },
  {
    "Type": "slowlog",
    "Label": "mysql-1",
    "URL": "http://10.0.0.12:19000/debug/log/slowlog",
    "Duration": 30
  }
]
```

Each item must have a non-empty `Type`, `Label`, and syntactically valid `URL`; `Duration` must be greater than zero. `ControlURL` is optional and must be an absolute URL when provided. The handler does not restrict `Type` to an enum, so unsupported types can be saved but cause group collection to fail when their internal route is called. Use `pprof`, `httplog`, and `slowlog` for grouped measurements. Although `memo` has a registered route, the group request has no `Text` field and therefore creates an empty memo. The settings file is stored as `data/targets.json`.

```sh
curl http://pprotein-host:9000/api/group/targets
curl -X POST -H 'Content-Type: application/json' \
  --data-binary @targets.json http://pprotein-host:9000/api/group/targets
curl http://pprotein-host:9000/api/group/collect
```

## Snapshot listing and bodies

`GET /api/pprof`, `/api/httplog`, `/api/slowlog`, and `/api/memo` return arrays of `Entry`. During startup, existing snapshot metadata is loaded and its processing status is rebuilt asynchronously.

For pprof, `GET /api/pprof/data/{id}` returns the stored response body, normally a gzip-compressed protobuf profile (`.pb.gz`). This is the artifact for `go tool pprof` or Go PGO workflows. `GET /api/pprof/data/latest` selects the newest successful profile. Add `?label=app-1` to select the newest successful profile for one label.

For logs, `GET /api/httplog/{id}` and `GET /api/slowlog/{id}` run ALP or SLP and return TSV text. The current handlers set `Content-Type: application/json` on these TSV responses. The corresponding `/data/{id}` paths return the raw captured log bytes. `/data/latest` uses the same latest-successful selection and optional `label` query parameter.

An unknown ID on a raw `/data/{id}` route returns `404`. An unknown ID on processed log and memo detail routes currently surfaces as `500`. A latest route returns `404` when it cannot find a successful matching snapshot.

### pprof web UI routes

The pprof processor starts the upstream Google pprof HTTP UI under `/api/pprof/{id}/`. The bundled pprotein interface embeds `/api/pprof/{id}/flamegraph`. Other paths and query parameters are provided by the pinned `github.com/google/pprof` dependency rather than implemented as pprotein REST resources.

## Memo API

Create a memo using:

```json
{
  "GroupId": "benchmark-2026-10-30",
  "Label": "run 1",
  "Text": "Score: 12345"
}
```

`POST /api/memo` returns `202 Accepted` with an empty body. The memo is immediately persisted and published to `/api/event`. Its snapshot type is `memo` and the ID suffix is `-memo.log`.

`GET /api/memo/{id}` returns:

```json
{"Text":"Score: 12345"}
```

`GET /api/memo` returns snapshot `Entry` objects and copies memo text to each entry's `Message` field.

## Configuration APIs

The configuration handlers share these semantics:

- `GET` returns the current file content.
- `POST` reads the entire request body, validates/parses it, persists the normalized content, and returns `200 OK` with no body.
- Invalid JSON or YAML returns `400 Bad Request`; storage failures return `500`.

`/api/httplog/config` stores `data/alp.yml`; `/api/slowlog/config` stores `data/slp.yml`. Their YAML structures are interpreted by ALP and SLP respectively. The current embedded defaults are in `internal/extproc/alp/alp.yml` and `internal/extproc/slp/slp.yml`. The handlers check YAML syntax but do not validate all ALP/SLP option semantics before saving.

The group target API stores `data/targets.json` and validates the list structure and required fields before saving.

## Event stream

Open `GET /api/event` as a Server-Sent Events stream. Each `message` event's `data` is a JSON-serialized `Entry`, including updates such as `pending` → `ok` or `fail`.

```js
const events = new EventSource("/api/event");
events.addEventListener("message", (event) => {
  const entry = JSON.parse(event.data);
});
```

The stream is live notification only; this implementation does not persist or replay old events. Fetch the list endpoints to load existing snapshots.

## Status codes

| Status | Where it is used |
| --- | --- |
| `200 OK` | Successful list/detail/config reads, config writes, collection request enqueue, and group collect enqueue; empty body for writes/triggers |
| `202 Accepted` | Memo created |
| `400 Bad Request` | Malformed collection or memo request body; invalid target-list JSON/schema; malformed YAML |
| `404 Not Found` | Missing raw snapshot body, no successful `latest` snapshot, or an unregistered route |
| `500 Internal Server Error` | Storage/processor failures; missing IDs on processed log and memo detail routes; group request dispatch failure |
| `409 Conflict` | Start requested while another session is active, stop requested before start finishes, or a second CPU profile requested in one Go process |
| `401 Unauthorized` | Target-side session control when `PPROTEIN_CONTROL_TOKEN` is set and the bearer token does not match |

Collection and processing errors that happen after the enqueue response do not change that original HTTP response. Read the entry state from a list endpoint or `/api/event`.

## Debug endpoints on pprotein and pprotein-agent

Both the main pprotein server and `pprotein-agent` mount the shared debug handler. The main server uses its `PORT` (default `9000`); the agent uses its `PORT` (default `19000`). The routes are intended as collection sources and are separate from `/api/*`.

| Method | Path | Behavior |
| --- | --- | --- |
| `GET` | `/debug/log/httplog?seconds=N` | Wait for `N` seconds and return bytes appended to the configured HTTP log during that interval |
| `GET` | `/debug/log/slowlog?seconds=N` | Wait for `N` seconds and return bytes appended to the configured slow log during that interval |
| `GET` | `/debug/fgprof` | Return an fgprof profile |
| `POST` | `/debug/capture/start` | Start CPU profiling or record a log file offset for a session |
| `POST` | `/debug/capture/stop` | Stop the session and return pprof/log bytes |
| `GET` | `/debug/pprof/` and `/debug/pprof/{name}` | Go `net/http/pprof` index and profile handlers |
| `GET` | `/debug/pprof/profile?seconds=N` | Capture a CPU profile for `N` seconds |
| `GET` | `/debug/pprof/trace?seconds=N` | Capture an execution trace for `N` seconds |
| `GET` | `/debug/pprof/cmdline` | Return process command-line arguments |
| `GET` | `/debug/pprof/symbol` | Go pprof symbol endpoint |

The log handlers default `seconds` to 30 when it is absent or not an integer. They seek to the current end of the file, wait, then return newly appended bytes; they do not return the whole log. If the request advertises `Accept-Encoding: gzip`, the response is gzip-compressed. Log paths are controlled by `PPROTEIN_HTTPLOG` (default `/var/log/nginx/access.log`) and `PPROTEIN_SLOWLOG` (default `/var/log/mysql/mysql-slow.log`). `PPROTEIN_GIT_REPOSITORY` selects the Git repository used for the `X-Git-Repository` response header (default `.`).

`pprotein-agent` is useful when logs reside on another machine: run it on that machine, make its port reachable from the pprotein server, and use its `/debug/log/...` URL as a target. For pprof, the target application must expose `/debug/pprof/...` or `/debug/fgprof` itself; the agent does not proxy another process's pprof endpoint.

## Embedding debug endpoints in Go applications

Import the integration package matching the application's HTTP framework. Echo v3, v4, and v5 adapters live in `integration/echo`, `integration/echov4`, and `integration/echov5`. The Echo v5 adapter requires Go 1.25 or newer.

```go
package main

import (
	"github.com/ultra-fast-gopher/pprotein/integration/echov5"
	"github.com/labstack/echo/v5"
)

func main() {
	e := echo.New()
	echov5.Integrate(e)
	if err := e.Start(":8080"); err != nil {
		panic(err)
	}
}
```

## Errors and operational notes

- The HTTP API has no authentication by default. Set `PPROTEIN_CONTROL_TOKEN` to protect target-side session controls; the main API still needs network restrictions or reverse-proxy authentication. The main server and agent listen on all interfaces by default (`:PORT`).
- Collection runs asynchronously. Check snapshot status after starting it; the initial `200` or `202` response is not a collection success result.
- Group target URLs are fetched by the pprotein server, so DNS, routing, firewall rules, and TLS must work from that host. The target client cannot attach custom authentication headers; protect access through network controls or an endpoint proxy.
- The app writes metadata, run state, and bodies below its `data/` work directory. Captures larger than 512 MiB per target are rejected. The `alp` and `slp` executables are needed to process their log types; Graphviz is needed for pprof views that render call graphs.
- Error response bodies use Echo's HTTP error handling and are not defined as a stable JSON error schema.

## Source map

- Routes and server startup: `cli/pprotein/main.go`, `cli/pprotein-agent/main.go`
- Collection, metadata, and persistence: `internal/collect/`, `internal/persistent/`, `internal/storage/`
- Group targets and collect trigger: `internal/collect/group/collector.go`
- pprof handlers and UI wiring: `internal/pprof/`
- ALP, SLP, memo handlers: `internal/extproc/`, `internal/memo/`
- Agent/debug routes and log-tail behavior: `integration/`, `internal/tail/`
- Bundled UI API usage: `view/src/`
