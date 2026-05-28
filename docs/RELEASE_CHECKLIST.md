# Release Checklist

Before public visibility changes or releases:

```bash
make ci
gitleaks detect --source . --redact
```

Checked on 2026-05-27 20:57 PDT:

- Local `make ci`: passed on initial scaffold before first public push.
- Working-tree secret scan: passed via `gitleaks detect --source . --no-git --redact`
  inside `make ci`.
- Full-history `gitleaks detect --source . --redact`: passed after initial
  local commit.
- GitHub visibility: public repository verified after first push.
- Public unauthenticated read: verified with `git ls-remote` and raw README
  fetch.
- GitHub Actions CI: passed on the first public push.
