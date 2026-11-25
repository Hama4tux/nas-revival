# Backend

Go service `nas-revival` (see [ADR 0002](../docs/adr/0002-web-ui-stack-debian.md) and
[API v1](../docs/api/v1.md)). Read-only stage: login, overview, SMART.

```text
cmd/nas-revival     main, set-password subcommand, self-signed TLS
internal/hal        procfs/sysfs/hwmon readers (no writes, no request-supplied paths)
internal/system     overview snapshot, systemd service states (fixed allowlist)
internal/smart      smartctl JSON summary, 10 min cache, does not wake sleeping disks
internal/auth       argon2id credentials, sessions, login rate limit
internal/api        HTTP routes, CSRF and security headers
internal/web        embedded UI (build tag `embedui`), SPA fallback
```

Checks: `go vet ./... && go test ./...` (Go from `~/sdk`). Full build for the NAS:
`scripts/webui/build.sh`. Deployment: [deploy/debian](../deploy/debian/README.md).
Local run: `nas-revival set-password -config DIR`, then
`nas-revival -listen 127.0.0.1:8443 -config DIR -state DIR2`.
