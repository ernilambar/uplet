# Changelog

## 1.0.1 - 2026-11-22
- Added: `too_many_redirects` reason code
- Added: `SHA256SUMS` file with release binaries
- Fixed: soft-404 detection missing streamed pages, pages over 64KB, and redirects from short paths like `/about`
- Fixed: SSRF blocklist gaps, including `::`, CGNAT, and private IPv4 embedded in NAT64/6to4 addresses
- Fixed: DNS rebinding against `serve` by rejecting requests whose `Host` isn't `127.0.0.1`
- Fixed: `serve` leaking connections after each check
- Fixed: false alerts from cross-host redirects, WAF-blocked default `User-Agent`, and unreachable first DNS address

## 1.0.0 - 2026-08-03
- Initial release
