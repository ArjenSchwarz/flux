# Flux CLI

`flux` is a read-only client for the existing Flux API. It makes one GET request
per invocation and writes the API's original JSON to stdout, adding a final
newline only when needed. Unknown fields, nulls, numeric precision, units and
timestamps are preserved. No local cache, polling or background service is used.

## Build and run

With the Go version declared in `go.mod` or newer:

```sh
make build-cli
bin/flux --help
```

Provide the HTTPS base URL via `FLUX_API_URL` or `--url`. Supply the existing
bearer token through `FLUX_API_TOKEN` in the process environment, or a protected
file using `--token-file /path/to/token`. An explicit URL overrides the environment;
the token file overrides `FLUX_API_TOKEN`. Surrounding token whitespace is trimmed.
There is no token argument, credential lookup, login flow or persistent setup.
Do not put tokens in command lines or commit credential files.

With the endpoint and token already supplied securely in the environment:

```sh
flux status
flux history                       # seven days, including Sydney today
flux history --days 14
flux history --from 2026-10-01 --to 2026-10-31
flux day 2026-10-31
flux status --url https://example.invalid --token-file /path/to/token
```

Options go **after the command and before the positional date**:
`flux day --token-file /path/to/token 2026-10-31`.

- `status` calls `/status`: live load, battery/solar/grid power, SoC, battery
  estimates, rolling averages and today's energy. `live: null` means no current
  reading, not zero load. The server requires a live reading to be at most 90
  seconds old; use its timestamp when answering current-state questions.
- `history` calls `/history`: daily summaries, SoC lows, peak-load periods,
  usage blocks and notes. `--days` accepts 1–31. `--from` and `--to` map to the
  API's inclusive `start`/`end` bounds, at most 31 days. They cannot be combined
  with `--days`. The server requires the explicit range to end before Sydney
  today. Longer comparisons need separate requests; there is no cursor pagination.
- `day` calls `/day?date=YYYY-MM-DD`: one day's summary and five-minute readings,
  peak-load periods, usage blocks and note. Past-day power is reconstructed from
  five-minute snapshots; today's points are downsampled readings. These are not
  a precise event/notification journal.

Dates and day boundaries use **Australia/Sydney**. Reading timestamps are RFC3339
UTC. Power is watts; daily energy is kWh, while peak-period energy is Wh. Positive
`pbat` means discharging; negative means charging. Positive `pgrid` means importing.
The CLI does not reinterpret these values or fill missing readings with zero.

Errors go to stderr, with no response body, credentials or raw transport error
printed. Exit codes are 0 for success/help, 1 for API/network/output failure, and 2
for invalid arguments or configuration. Requests time out after 30 seconds, do not
follow redirects, and accept JSON responses up to 8 MiB. API errors include the
HTTP status; a 400 can mean the server rejected a date relative to Sydney today.

## Linux artifacts

The existing Poller CI workflow tests Go code and uploads `flux-linux-<workflow SHA>`
on matching PR/main builds. It contains `flux-linux-amd64.tar.gz` and
`flux-linux-arm64.tar.gz`; each archive contains an executable named `flux`.
The tar archives preserve executable permissions when downloaded through Actions.
Artifacts have a 90-day retention period, not permanent release storage.

Download the artifact from a successful workflow run's **Artifacts** section, or:

```sh
gh run download RUN_ID --repo ArjenSchwarz/flux --name flux-linux-WORKFLOW_SHA --dir /tmp/flux-artifact
tar -xzf /tmp/flux-artifact/flux-linux-amd64.tar.gz -C /tmp/flux-artifact
/tmp/flux-artifact/flux --help
```

Use amd64 for x86_64 Linux, arm64 for aarch64. `make build-cli-linux` builds the same
archives locally. Endpoint network access and secure token delivery on the cloud
computer must be configured separately; no credentials are included in the binaries.
