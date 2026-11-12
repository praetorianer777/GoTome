# Metrics

GOtome can write its numbers in the text format Prometheus reads, for
Prometheus, Grafana Alloy, VictoriaMetrics or anything that scrapes it. It
is off until you switch it on, and reachable only with an API key made for
it.

## Switching it on

1. As an administrator, set **Prometheus metrics** to on on the Settings page
   (`metrics.enabled`). Off, `GET /api/v1/metrics` answers 404 to everyone.
2. On the administrator's profile make an API key with only the **Read the
   Prometheus metrics** scope (`metrics`). That scope reaches this one route
   and nothing else, and an `admin` key does not reach it.
3. Point the scraper at `/api/v1/metrics` with the key as a bearer token
   ([the API and API keys](api.md)).

The endpoint lives under `/api/v1` with the rest of the API, so the same
reverse proxy, sub-path and TLS cover it. Do not publish it to the internet
if the scraper runs next to GOtome; the key is the only door.

```yaml
scrape_configs:
  - job_name: gotome
    scheme: https
    metrics_path: /api/v1/metrics
    authorization:
      credentials_file: /etc/prometheus/gotome.key
    scrape_interval: 30s
    static_configs:
      - targets: ["books.example.org"]
```

Put the key (`gotome_...`) alone in the file. Counts read from the database
are reused for 15 seconds, so a scrape every few seconds costs it little.

## What is in it

No title, author, name or path appears: the labels are fixed words (queue
names, job kinds, states) and the route patterns of GOtome's API.

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `gotome_build_info` | gauge | `version` | the running version |
| `gotome_books` | gauge | | books, placeholders and merged or deleted ones left out |
| `gotome_libraries` | gauge | | libraries |
| `gotome_users` | gauge | `state` | accounts, `active` or `disabled` |
| `gotome_files` | gauge | `extract_state`, `presence` | files not in the trash |
| `gotome_jobs` | gauge | `queue`, `state` | jobs `available`, `running`, `retryable` or `scheduled` |
| `gotome_jobs_failed` | gauge | `queue`, `kind` | jobs that failed for good and are still on record |
| `gotome_search_outbox_rows` | gauge | | changes the full-text index has not applied yet (its lag) |
| `gotome_scans` | gauge | `state` | scans on record |
| `gotome_last_scan_duration_seconds` | gauge | | the latest finished scan |
| `gotome_webhook_deliveries` | gauge | `state` | webhook deliveries on record |
| `gotome_db_pool_*` | gauge, counter | | the database pool: connections, acquires, waits |
| `gotome_http_requests_total` | counter | `route`, `method`, `class` | requests by route pattern such as `/api/v1/books/{id}` and status class |
| `gotome_http_request_duration_seconds` | histogram | `route`, `method`, `class` | how long they took |

Request counters start at zero when GOtome starts, as Prometheus expects.
Extraction durations are not exported separately: `gotome_jobs` and
`gotome_jobs_failed` show the extract queue's backlog and failures.

## Grafana

Add Prometheus as a data source and graph, for example:

- `sum by (queue) (gotome_jobs{state="available"})` for the backlog of each queue;
- an alert on `sum(gotome_jobs_failed) > 0`: it is a gauge of the failed jobs still on
  record, which the queue removes after a while;
- `histogram_quantile(0.95, sum by (le, route) (rate(gotome_http_request_duration_seconds_bucket[5m])))`
  for the slowest routes;
- `gotome_search_outbox_rows > 1000 for 15m` as an alert that the index is behind.
