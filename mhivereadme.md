# gogcli Project Reference

## Overview

`gog` is a task-first Google Workspace CLI for Gmail, Calendar, Drive, Docs,
Sheets, Slides, Forms, Meet, Apps Script, Analytics, Search Console, Contacts,
Tasks, Classroom, Chat, YouTube, and Workspace admin.

The project emphasizes predictable automation:

- Stable `--json` and `--plain` stdout for scripts and agents.
- Human hints, progress, and warnings on stderr.
- `--no-input` for non-interactive environments.
- Runtime safety controls such as `--readonly`, command allow/deny lists,
  `--gmail-no-send`, dry-run plans, and untrusted-content wrapping.
- A typed MCP server that is read-only by default.

## Repository Layout

- `cmd/gog/`: CLI entrypoint.
- `internal/`: implementation.
- `internal/cmd/`: command implementations, including MCP commands.
- `internal/googleauth/`, `internal/authclient/`, `internal/secrets/`: OAuth,
  auth client, and keyring support.
- `internal/integration/`: opt-in integration tests behind build tags.
- `docs/`: specs, generated command docs, release docs, and feature guides.
- `scripts/`: release helpers and `scripts/gog.mjs`.
- `bin/`: local build outputs.
- `testdata/`: fixtures.

## Build And Test

- `make` or `make build`: build `bin/gog`.
- `make tools`: install pinned development tools into `.tools/`.
- `make fmt`: run `goimports` with local prefix `github.com/steipete/gogcli`
  and `gofumpt`.
- `make lint`: run lint checks.
- `make test`: run unit tests.
- `make ci`: run the full local gate.
- `pnpm gog ...`: optional build-and-run shortcut.
- `lefthook install`: enable pre-commit and pre-push checks.

Unit tests live beside code as `*_test.go`. Integration tests are local-only:

```bash
GOG_IT_ACCOUNT=you@gmail.com go test -tags=integration ./internal/integration
```

Integration tests require OAuth client credentials and a stored refresh token in
the configured keyring.

## CLI And MCP Surface

Use generated docs and schema instead of guessing flags:

- `gog --help`
- `GOG_HELP=agent gog --help`
- `gog schema --json`
- `docs/commands/README.md`
- `docs/commands/gog-mcp.md`
- `docs/mcp.md`
- `docs/agent-skills.md`
- `docs/safety-profiles.md`

Automation should prefer explicit account selection and parseable output:

```bash
gog --readonly --account user@example.com gmail search 'newer_than:7d' --json --wrap-untrusted
```

For Google content reads, prefer `--json --wrap-untrusted`; for Gmail body
inspection, prefer `--sanitize-content`.

## Auth And Secrets

Never commit OAuth client credential JSON files, refresh tokens, access tokens,
or keyring passwords.

Use OS keychain backends for local use. For headless services, the file keyring
is acceptable only with a non-interactive secret supplied safely to the service
environment:

- `GOG_KEYRING_BACKEND=file`
- `GOG_KEYRING_PASSWORD`
- `HOME`

All API keys, tokens, passwords, OAuth client secrets, and credentials must live
in 1Password. Do not print, read into chat, or store secret values in shell
variables. Use `op read`, `op inject`, or `op run` in pipelines or subprocesses
so secrets are passed directly to their target.

## Development Rules

- Keep stdout parseable for `--json` and `--plain`.
- Send human hints and progress to stderr.
- Treat Gmail label IDs as case-sensitive opaque tokens.
- Only case-fold label names for name lookup.
- Prefer structured APIs and generated schema/docs over ad hoc parsing.
- Use `gog` for Google Docs work; use `docx-js` only when explicitly creating a
  `.docx` Word file.

## Commit And PR Workflow

- Create commits with `committer "<msg>" <file...>`; avoid manual staging.
- Use Conventional Commits with action-oriented subjects.
- Group related changes and avoid unrelated refactors.
- PR summaries should include scope, testing performed, and user-facing changes.

For PR links, default to review mode: inspect with `gh pr view` and
`gh pr diff`, do not switch branches, and do not change code.

Landing contributor work requires a temporary branch from `main`, bringing in
the PR, fixing as needed, updating `CHANGELOG.md`, running `make ci`, committing,
merging to `main`, deleting the temporary branch, and ending on `main`.
Preserve contributor credit with `Co-authored-by:` trailers and a PR comment
with what landed and SHAs.

## Per-agent Instruction Files

- `AGENTS.md`: Codex-specific repository rules and pointer to this file.

