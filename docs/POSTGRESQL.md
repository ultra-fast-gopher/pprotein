# PostgreSQL slow query collection

Use the `pgslowlog` target to collect PostgreSQL text logs and view an aggregated
SLP report. MySQL continues to use `slowlog`; both can participate in the same
fixed-duration collection or start/stop session. The PostgreSQL report is sorted
by total query time, descending, by default.

## 1. Enable duration logs

On the PostgreSQL server, inspect the current settings and log destination:

```sql
SHOW log_min_duration_statement;
SHOW log_line_prefix;
SHOW log_destination;
SHOW logging_collector;
SELECT pg_current_logfile();
```

Set a threshold in PostgreSQL's managed configuration. For a benchmark where
every completed query should contribute to the report, use:

```conf
log_min_duration_statement = 0
log_parameter_max_length = 0
```

Reload the configuration with `SELECT pg_reload_conf();` and check the effective
values in a new connection. These settings do not require a server restart.
Use a higher threshold, such as `100ms`, when collecting every query produces too
much log I/O. Queries below that threshold will be absent from the report.

Use a text log (`stderr`); CSV and JSON logs are not supported by this processor.
An existing text log can be used without enabling `logging_collector`. For
example, Debian/Ubuntu installations often redirect stderr to
`/var/log/postgresql/postgresql-16-main.log`. `pg_current_logfile()` returns NULL
when the collector is disabled, even if the server's stderr is saved elsewhere.

`log_parameter_max_length = 0` suppresses bind parameter values in normal
statement logs. It does not redact values written directly into SQL or remove
them from already captured logs. Restrict access to logs and the collection API.

## 2. Run the agent on the database host

The agent needs read access to the log file and read/search access to its parent
directories. Grant only the required access; do not make database logs publicly
readable. For example, a systemd service can run as an existing unprivileged user
with the log reader group in `SupplementaryGroups`.

```sh
HOST=127.0.0.1 \
PORT=19000 \
PPROTEIN_PGSLOWLOG=/var/log/postgresql/postgresql-16-main.log \
pprotein-agent
```

`HOST` defaults to an empty string (all interfaces). Bind to a trusted private
interface when pprotein runs on another machine, or use a protected tunnel for a
loopback listener. Do not expose the debug endpoints to the public Internet.

`PPROTEIN_PGSLOWLOG` defaults to `/var/log/postgresql/postgresql.log`. It is
independent of `PPROTEIN_SLOWLOG`, which still selects the MySQL log.

The PostgreSQL source endpoint is `/debug/log/pgslowlog?seconds=N`. Session
control uses the existing `/debug/capture/start` and `/debug/capture/stop`
endpoints with `"Type": "pgslowlog"`. Set `PPROTEIN_CONTROL_TOKEN` on both the
collector and agent to authenticate session controls; fixed-duration debug log
endpoints still require network access restrictions.

## 3. Configure the report and target

The collector requires an `slp` executable with the `pg` subcommand in PATH.
SLP v0.2.3 has been used to verify this integration. The PostgreSQL configuration
is exposed at `/api/pgslowlog/config` and stored as `data/pgslp.yml`.

Set `log_line_prefix` to match `SHOW log_line_prefix` on the source server,
including spaces. The default is:

```yaml
bundle_values: true
bundle_where_in: true
filters: Query matches "(?i)^\\s*(SELECT|INSERT|UPDATE|DELETE|WITH|LOCK|BEGIN|COMMIT|ROLLBACK)\\b"
log_line_prefix: '%m [%p] '
sort: sum-query-time
reverse: true
limit: 65536
```

For Debian's common prefix, use `log_line_prefix: '%m [%p] %q%u@%d '` instead.
A prefix mismatch can produce an empty report. Set the correct configuration
before collecting; already processed snapshots use cached reports.

Add a target through the settings page (`group/targets`) or its API:

```json
{
  "Type": "pgslowlog",
  "Label": "postgresql-1",
  "URL": "http://database-private-host:19000/debug/log/pgslowlog",
  "Duration": 30
}
```

`POST /api/group/targets` replaces the entire array, so retain other targets.
Use the PostgreSQL tab to collect this source directly, or the group controls
to collect it with other targets. Start/stop sessions use `POST /api/runs` and
`POST /api/runs/{id}/stop`, just like the other supported types.

The PostgreSQL endpoints mirror the MySQL endpoints:

| Method | Path | Result |
| --- | --- | --- |
| GET | `/api/pgslowlog` | Snapshot list and processing status |
| POST | `/api/pgslowlog` | Enqueue a fixed-duration collection |
| GET | `/api/pgslowlog/{id}` | Aggregated TSV report |
| GET | `/api/pgslowlog/data/{id}` | Original captured log |
| GET | `/api/pgslowlog/data/latest` | Latest successful log, optionally filtered by `label` |
| GET / POST | `/api/pgslowlog/config` | Read / replace the SLP YAML configuration |

## Prepared statements and interpretation

PostgreSQL writes simple-protocol queries as `statement:` and extended-protocol
execution as `execute <name>:`. The processor normalizes the latter for SLP
without modifying the stored raw log. Parse and bind duration lines are not
counted as additional query executions. Multiline SQL, including statements
that start on the following log line, is preserved. `LOCK` and transaction
statements are included so lock waits and slow commits remain visible. Normal
SLP query abstraction groups equivalent statements.

The report measures the duration of completed executions present in the capture.
A query that began before capture started can be included if it finished during
capture; one that finishes after capture stops is absent. Empty captures return
no data in a start/stop session. Sampling or duration thresholds make counts
partial. These are log-based measurements, independent of `pg_stat_statements`.

For database logging behavior, see the PostgreSQL
[logging reference](https://www.postgresql.org/docs/16/runtime-config-logging.html).
For SLP's PostgreSQL support and configuration, see the
[SLP documentation](https://github.com/tkuchiki/slp).
