# open-workstation-mcp - Agent Instructions

> Canonical instructions: AGENTS.md

Public-safe Go sample for MCP-style workstation automation patterns. Every
fixture, example, and commit must preserve the synthetic, local-only, dry-run
boundary.

## Public Boundary

- Use only synthetic app names, requests, fixtures, and dry-run examples.
- Do not add private workstation paths, usernames, hostnames, account data,
  cookies, browser profiles, OAuth tokens, dotfiles contents, screenshots,
  clipboard data, WiFi details, secrets, or copied private repo implementation
  details.
- Do not add live desktop automation that changes local state without explicit
  dry-run gates.

## Verification

- Run `make ci` before committing meaningful changes.
- Run `gitleaks detect --source . --redact` before publishing or major public
  example expansion.
- Keep README, examples, and tool outputs aligned with `PUBLIC_BOUNDARY.md`.

## Landing

- Land verified changes via `branch -> PR -> claude-harness/bin/hg-land --repo <checkout>` (never `git push origin main` directly; never use `git add -A`).

<!-- BEGIN GENERATED LANDING POLICY -->
Use `hg-land --repo <checkout>` (`--no-verify` while the fleet gate switch
`~/.config/hg/gates-disabled` or `HG_GATES_DISABLED=1` is active; `--local-gate <log>`
under `HG_GATES_DISABLED=0`, or `hg-gate-then-land --require-gate`) to land through a
pull request. Owned by `claude-harness/.agents/policies/landing.md`.

- While fleet gates are disabled (operator 2026-11-08), `hg-land` lands without a
  local gate log; when gated, bind the log to `head=<full SHA>` with final `rc=0`.
- Observe the repository's current remote requirements. Required checks and
  branch protections remain binding; hosted CI is not assumed required everywhere.
- Never bypass a failed gate, force-push, or replace unrelated work. Do not use
  direct pushes to main as a substitute for the configured landing route.
- Optoinvest repositories remain local-only: no remote publication.
- Prove landing with the tool receipt and exact remote main SHA. Source updates
  and runtime/deployment verification are separate claims.
<!-- END GENERATED LANDING POLICY -->
