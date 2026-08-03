# Uplet — Implementation Plan

---

## Phase 0 — Project Scaffolding

- `go mod init github.com/ernilambar/uplet`
- Layout: `cmd/uplet/main.go`, `internal/checker/`, `internal/server/`, `internal/cli/`
- `Makefile`: `build`, `run`, `test`, `install`

**Deliverable:** compiles, `uplet --help` works.

---

## Phase 1 — Core Checking Logic (`internal/checker`)

```go
type Result struct {
    URL            string `json:"url"`
    ValidFormat    bool   `json:"valid_format"`
    SiteUp         bool   `json:"site_up"`
    PageExists     bool   `json:"page_exists"`
    StatusCode     int    `json:"status_code"`
    ResponseTimeMs int64  `json:"response_time_ms"`
    Reason         string `json:"reason"`
    ReasonCode     string `json:"reason_code"`
    RedirectedTo   string `json:"redirected_to,omitempty"`
    Attempts       int    `json:"attempts"`
}
```

- `reason_code` is a stable enum (`ok`, `not_found`, `redirect_not_found`, `client_error`, `server_error`, `network_error`, `dns_error`, `timeout`, `invalid_format`, `blocked_target`, `too_large`, `tls_error`) — clients match on this, not on the free-text `reason`.
- Schema is additive-only within `/v1`; a breaking change ships as `/v2`.

### Format validation
- `net/url`, require `http`/`https` scheme + host. No other schemes.

### DNS resolution
- Resolution happens **inside the dialer** (see below), not as a separate pre-check. This is deliberate: a separate `net.LookupHost` followed by handing the URL to the HTTP client creates a TOCTOU window — the client re-resolves independently and can connect to a different IP than the one validated (DNS rebinding). We never let that happen.
- Distinguish NXDOMAIN (`dns_error`, definitive) from resolver timeout (`timeout`).

### SSRF protection — pinned-dialer model (REQUIRED, not opt-in)
The server has no auth; it must never double as an open internal-network proxy or port scanner. Protection is enforced at the transport layer so it cannot be bypassed by rebinding or redirects.

- **Custom `DialContext`:** resolve the host, validate **every** resolved IP against the block list, then dial the **exact validated IP** — the address the client connects to is provably the address that was checked. No second resolution, no TOCTOU gap.
- **Block list (checked against resolved IP, never the hostname string):**
  - Loopback `127.0.0.0/8`, `::1`
  - Private RFC 1918 (`10/8`, `172.16/12`, `192.168/16`)
  - Link-local `169.254.0.0/16` (covers cloud metadata `169.254.169.254`), `fe80::/10`
  - Unique-local `fc00::/7` (covers IPv6 metadata `fd00:ec2::254`)
  - Unspecified/broadcast `0.0.0.0/8`, `255.255.255.255`
  - **IPv4-mapped IPv6** (`::ffff:0:0/96`) — unmap before checking so `::ffff:127.0.0.1` cannot smuggle a loopback address past the CIDR test.
  - Alternate host encodings (octal/decimal/hex) are neutralized automatically because we validate the *parsed `net.IP`*, not the URL string.
  - → `reason_code=blocked_target`, connection refused before any bytes sent.

### Redirect handling — SSRF re-validated per hop
- **`CheckRedirect` re-runs the full block-list validation on every hop's target**, up to 10 hops. A public URL that `302`s to `http://169.254.169.254/` or `http://10.0.0.1/` is rejected at the redirect boundary — redirects do not escape SSRF protection.
- Transparent redirects (scheme upgrade, `www.` add/drop, trailing-slash normalization) pass through as normal and are **not** treated as soft-404s.

### HTTP inspection
- `HEAD` first, `GET` fallback on 405.
- **Timeouts (mandatory):** per-attempt `context` deadline **and** a `Transport` with `DialContext` timeout, `TLSHandshakeTimeout`, and `ResponseHeaderTimeout`. No unbounded operation exists anywhere — slow-loris and hung-target DoS are structurally impossible.
- **Response body cap:** `GET` fallback reads at most N KB via `io.LimitReader` for soft-404 body inspection; over the cap → `reason_code=too_large` (never OOM on a hostile large body). `HEAD` reads no body.
- **TLS policy (explicit):** certificates **are verified** — no `InsecureSkipVerify`. A bad/expired/self-signed cert is a real failure → `reason_code=tls_error`, `site_up=false`. Stated here so results are unambiguous.

### Soft-404 detection — two modes, honestly scoped
- **Redirect-based:** if the effective URL's normalized host+path differs from the original (beyond the transparent cases above), report `page_exists=false` / `reason_code=redirect_not_found` with `redirected_to` set.
- **Content-based:** on a `200 OK`, inspect the (capped) body for the common "soft 404" pattern — a page that returns success while saying the resource does not exist. This is best-effort and heuristic; it is documented as such, not sold as authoritative.
- Redirect normalization is conservative to avoid false `redirect_not_found` on legitimate `/`→`/home`, locale-prefix, or trailing-slash redirects.

### Retry
- Max 2 attempts, 500ms backoff, **only** for dial timeout / connection-reset / TLS-handshake transient failures.
- **Not retried:** DNS/NXDOMAIN, format errors, `blocked_target`, connection-refused (definitive), any HTTP status.
- `attempts` and `response_time_ms` recorded per check.

### Reason-code semantics
- Definitive-down (NXDOMAIN, connection refused, cert invalid) → distinct from **indeterminate** (timeout). Timeout is its own `timeout` code so CLI/monitoring can treat it as UNKNOWN rather than CRITICAL.

### Unit tests (`httptest.Server`)
- 200, 404, 500, 405→GET fallback, transparent redirect, soft-404 redirect, content-based soft-404, flaky-then-success, exhausted retries.
- DNS: NXDOMAIN vs resolver timeout.
- **SSRF: redirect-to-private-IP rejected, rebinding (public→private on re-resolve) rejected, IPv4-mapped-IPv6 loopback rejected, metadata IP rejected.**
- Timeout enforced (hung server), oversized body capped, TLS-error path.

**Deliverable:** `checker.Check(ctx, url string) Result`, fully tested standalone, SSRF-safe by construction.

---

## Phase 2 — CLI Mode (`uplet check <url>`)

- Terminal output: status indicator, status code, response time, reason.
- Exit codes (Nagios-style): `0` OK, `1` WARNING (site up, page missing), `2` CRITICAL (definitively down/unregistered/blocked), `3` UNKNOWN (bad format **or** timeout — indeterminate, not proven down).
- `--json` flag, same schema as the API.
- `--timeout` flag for the per-check deadline (sane default, e.g. 10s).

**Deliverable:** `uplet check https://example.com` works end-to-end.

---

## Phase 3 — Server Mode (`uplet serve --port <port>`)

- Route: `POST /v1/check`. Versioned now so a future breaking change ships as `/v2/check` without disrupting `/v1`.
- Body: `{"url": "..."}` → `checker.Check` → JSON response.
- **`Content-Type: application/json` enforced.** This is a security control, not a nicety: it forces a CORS preflight for cross-origin requests, so a drive-by page cannot POST a `text/plain` body that fires the outbound-request side effect without the browser first asking permission (which the server never grants). Wrong/missing content type → `415`.
- **`http.MaxBytesReader` on the request body.** Bounded JSON decode — no memory exhaustion from a giant POST.
- Malformed request (bad JSON / missing `url`) → `{"error": "missing_url"}` at `400`.
- **Concurrency cap** on in-flight checks (bounded worker/semaphore) — the SSRF-safe checker still costs sockets; unbounded parallel requests are a self-DoS. Excess → `429`.
- `--port` flag, default `54321`. Binds to `127.0.0.1` only — never `0.0.0.0`, never the hostname `localhost` (avoids IPv4/IPv6 resolution ambiguity).
- `127.0.0.1:54321` is the canonical address — use it verbatim in docs, curl examples, and any Chrome extension `host_permissions` entry (extension permissions match on the literal host string, not the resolved IP, so `localhost` and `127.0.0.1` are not interchangeable there).
- **CORS + SSRF are complementary, not redundant.** No permissive `Access-Control-Allow-Origin` is sent, so a browser page can never *read* a response. But CORS alone does **not** stop the outbound-request side effect from firing — only the enforced JSON content type (preflight) plus the transport-layer SSRF protection (Phase 1) do that. A Chrome extension's service worker bypasses CORS via declared `host_permissions` and is unaffected.
- Graceful shutdown on SIGINT/SIGTERM; basic request logging.

**Deliverable:** `curl -X POST 127.0.0.1:<port>/v1/check -H 'Content-Type: application/json' -d '{"url":"..."}'` returns correct schema.

---

## Phase 4 — Packaging & Docs

- `README.md`: usage for `check` and `serve`; explicit note on the SSRF/TLS/CORS security model.
- Binaries built and attached to GitHub Releases.
