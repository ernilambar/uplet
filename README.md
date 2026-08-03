# uplet

uplet checks whether a URL is up and whether the page it points to actually
exists — distinguishing "the site is down" from "the site is up but this
specific page is gone" (including soft 404s). It ships as a CLI and as a
small loopback-only HTTP server, and is SSRF-safe by construction, not by
opt-in configuration.

## Install

### From a release

macOS only (arm64 or amd64). Download the binary from
[Releases](https://github.com/ernilambar/uplet/releases), then:

```sh
chmod +x uplet-darwin-<arch>
mv uplet-darwin-<arch> /usr/local/bin/uplet
```

### From source

```sh
git clone https://github.com/ernilambar/uplet
cd uplet
make build     # -> build/uplet
make install   # go install ./cmd/uplet
```

## Usage

### `uplet check`

```
uplet check <url> [--json] [--timeout <duration>]
```

```console
$ uplet check https://example.com
OK  https://example.com
  status_code:    200
  response_time:  68ms
  reason:         ok (ok)

$ uplet check --json https://example.com/missing-page
{
  "url": "https://example.com/missing-page",
  "valid_format": true,
  "site_up": true,
  "page_exists": false,
  "status_code": 404,
  "response_time_ms": 39,
  "reason": "page not found",
  "reason_code": "not_found",
  "attempts": 1
}
```

Exit codes (standard monitoring-plugin convention):

| Code | Meaning  | When                                              |
| ---- | -------- | -------------------------------------------------- |
| 0    | OK       | site up, page exists                               |
| 1    | WARNING  | site up, page missing (404, soft-404, other 4xx)   |
| 2    | CRITICAL | definitively down / unregistered / blocked         |
| 3    | UNKNOWN  | bad URL format, or indeterminate (timeout)         |

### `uplet serve`

```
uplet serve [--port <port>]   # default 54321, binds to 127.0.0.1 only
```

```console
$ curl -X POST 127.0.0.1:54321/v1/check \
    -H 'Content-Type: application/json' \
    -d '{"url":"https://example.com"}'
{"url":"https://example.com","valid_format":true,"site_up":true,"page_exists":true,"status_code":200,"response_time_ms":56,"reason":"ok","reason_code":"ok","attempts":2}
```

The response schema is the same as `--json` output above and is
additive-only within `/v1` — a breaking change ships as `/v2/check`.
`reason_code` is the stable field to match on; `reason` is free text for
humans. Possible values: `ok`, `not_found`, `redirect_not_found`,
`client_error`, `server_error`, `network_error`, `dns_error`, `timeout`,
`invalid_format`, `blocked_target`, `too_large`, `tls_error`.

Error responses from the server itself (not the checker):

| Status | Body                              | Cause                                |
| ------ | ---------------------------------- | ------------------------------------- |
| 400    | `{"error":"missing_url"}`          | malformed JSON or missing `url`       |
| 405    | `{"error":"method_not_allowed"}`   | non-POST request                      |
| 415    | `{"error":"unsupported_media_type"}` | missing/wrong `Content-Type`        |
| 429    | `{"error":"too_many_requests"}`    | concurrency cap exceeded              |

## Security model

The server has no authentication, so it must never double as an open
internal-network proxy or port scanner. This is enforced at the transport
layer, not as an opt-in setting:

- **SSRF.** Every dial resolves the host and validates *every* resolved IP
  against a block list — loopback, RFC 1918 private ranges, link-local
  (including cloud metadata `169.254.169.254`), IPv6 unique-local
  (including metadata `fd00:ec2::254`), and unspecified/broadcast — before
  connecting to the exact validated IP. Resolution and dial happen as one
  atomic step, so there is no DNS-rebinding window. Redirects are
  re-validated at every hop, up to 10.
- **TLS.** Certificates are verified; there is no `InsecureSkipVerify`. An
  invalid, expired, or self-signed certificate is a real failure
  (`tls_error`), never silently ignored.
- **CORS.** No permissive `Access-Control-Allow-Origin` is ever sent, so a
  browser page can't read a response cross-origin. That alone doesn't stop
  the outbound request from firing, though — enforcing
  `Content-Type: application/json` (which forces a CORS preflight) plus
  the SSRF transport protection above are what actually stop a drive-by
  page from using the server as a proxy.
- **Bounds.** Request bodies are size-capped (`http.MaxBytesReader`),
  in-flight checks are capped by a semaphore (`429` beyond it), and every
  HTTP attempt has dial/TLS-handshake/response-header timeouts plus a
  capped body read for soft-404 inspection — no unbounded operation exists
  anywhere.

`uplet serve` binds to `127.0.0.1` only — never `0.0.0.0`, never the
hostname `localhost`. Use `127.0.0.1:54321` verbatim in scripts and any
browser-extension `host_permissions` entry: permission grants match the
literal host string, not the resolved IP.

## Development

```sh
make build   # build/uplet
make test    # go test ./...
make run ARGS="check https://example.com"
```
