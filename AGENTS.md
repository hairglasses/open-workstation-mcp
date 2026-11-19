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

- Land verified changes via feature branch and Pull Request with passing CI checks; never push directly to main or stage with git add -A.
