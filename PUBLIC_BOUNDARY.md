# Public Boundary

`open-workstation-mcp` is a public-safe reference slice. Keep it limited to
synthetic desktop/workstation examples and dry-run planning.

Allowed:

- Synthetic app names such as `terminal`, `editor`, or `browser`.
- Generic Linux/Wayland concepts such as focus, input, clipboard, and config drift.
- Deterministic CLI examples and tests.
- Public-safe docs about review gates, dry-run behavior, and output budgets.

Not allowed:

- Real workstation paths, usernames, hostnames, cookies, browser profiles, OAuth
  tokens, dotfiles contents, screenshots, clipboard data, WiFi details, or secrets.
- Private repository names or implementation details from internal systems.
- Live desktop automation that changes local state without explicit dry-run gates.
