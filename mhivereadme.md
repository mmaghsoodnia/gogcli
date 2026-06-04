# gogcli project reference

`gogcli` builds the `gog` command-line tool and MCP server for Google Workspace automation. It is designed for terminals, scripts, CI, and coding agents that need typed Google tools without a generic command runner.

## Architecture

- `cmd/gog/` contains the CLI entrypoint.
- `internal/cmd/` contains command implementations, including the MCP server and typed MCP tool schemas.
- `internal/googleapi/`, `internal/googleauth/`, `internal/config/`, `internal/secrets/`, `internal/outfmt/`, and `internal/ui/` hold Google API clients, OAuth/keyring handling, configuration, output formatting, and user interaction helpers.
- `docs/commands/` contains generated command reference pages.
- `docs/mcp.md` documents the MCP server contract.
- `scripts/` contains release/docs helpers.
- `bin/` and root `gog` are local build outputs.

## Build and test

- `make` or `make build` builds `bin/gog`.
- `make fmt` runs Go formatting (`goimports` with local prefix `github.com/steipete/gogcli`, plus `gofumpt`).
- `make lint`, `make test`, and `make ci` are the normal local gates.
- Unit tests use Go's standard `testing` package.
- Integration tests are opt-in: `GOG_IT_ACCOUNT=you@gmail.com go test -tags=integration ./internal/integration`.

## MCP server

`gog mcp` exposes typed, allowlisted tools over stdio. It is read-only by default. Write tools require `--allow-write` and still must match `--allow-tool` filters.

The MCP server must not expose a generic shell or arbitrary `gog` command runner. Each tool should map to one reviewed operation with a fixed schema, typed arguments, and unknown-field rejection handled by the MCP layer.

When adding MCP tools:

- Add the tool spec in `internal/cmd/mcp_tools.go`.
- Set the correct risk (`mcpRiskRead` or `mcpRiskWrite`).
- Build child `gog` arguments from typed fields only.
- Reject ambiguous or dangerous argument combinations.
- Add focused tests in `internal/cmd/mcp_test.go`.
- Update `docs/mcp.md` and generated command docs if the command schema changes.

## Output and safety

- Keep stdout parseable for `--json` and `--plain`.
- Send human hints and progress to stderr.
- Treat Gmail label IDs as case-sensitive opaque tokens; only case-fold label names for lookup.
- Never commit OAuth client credential JSON, refresh tokens, keyring files, or service-account keys.
- Prefer OS keychain backends. Use `GOG_KEYRING_BACKEND=file` with `GOG_KEYRING_PASSWORD` only for headless environments, and inject that password from 1Password rather than exposing it.

## Release and PR workflow

- Use Conventional Commit style with action-oriented subjects.
- Group related changes; avoid unrelated refactors.
- PRs should summarize scope, tests run, and user-facing changes or new flags.
- If landing contributor work, preserve author credit with `Co-authored-by:` trailers and note what landed.
- When reviewing a PR link, inspect it without switching branches or changing code unless explicitly asked to land/fix it.

## Per-agent instruction files

- `AGENTS.md` — Codex/agent-specific pointer. Project knowledge belongs here in `mhivereadme.md`, not in `AGENTS.md`.
