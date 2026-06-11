# gogcli (mhive fork) — project reference

Single source of truth for this checkout. Any agent reads this first.

`gog` is a Go CLI for Google Workspace (Gmail, Calendar, Drive, Docs, Sheets,
Slides, Contacts, and more). This repo is **our fork** of the upstream project,
carrying a small set of local patches on top of upstream releases.

---

## Repo identity

| | |
|---|---|
| Go module path | `github.com/steipete/gogcli` (upstream's path — do **not** rename) |
| `origin` | `https://github.com/mmaghsoodnia/gogcli.git` (our fork) |
| `upstream` | `https://github.com/openclaw/gogcli.git` |
| Binary | `gog` (built to `bin/gog`) |
| Entrypoint | `cmd/gog/main.go` |

The module path stays `steipete/gogcli` because changing it would break every
internal import and diverge us from upstream. Our fork lives at the `origin`
remote; releases come from `upstream`.

---

## Current state (as of 2026-06-10)

`main` is **synced to upstream `v0.23.0`** plus **3 local patches**.

- HEAD: `cf31fe12` — `git describe` → `v0.23.0-16-gcf31fe12`
- Upstream base: `5ce1de77` (upstream/main, `v0.23.0`)

**Local patches carried on top of upstream** (`git log upstream/main..main`):

| Commit | Patch | Why it's ours |
|---|---|---|
| `e8b859e6` | Add `webContentLink` to drive upload output | Upstream's `drive upload` only returns `webViewLink`; we also emit the direct download link (needed for inserting images into Docs). |
| `3394fa3a` | `docs comments list --show-anchors` + comment help text | Exposes kix anchor IDs from UI-created comments — the only known workaround for inline comment anchoring. No upstream equivalent. |
| `cf31fe12` | gitignore `gog-linux-amd64` | Local build-artifact ignore. |

**Dropped during the v0.23.0 sync:** a local rich-formatting commit
(`docs format` font/color/alignment). Upstream built a superior `docs format`
command independently, so ours was retired to avoid regressing the docs command
set. It is preserved on branch `backup/main-pre-sync-20260610` if ever needed.

---

## Reproducing this build on another machine

**Prerequisites:** Go ≥ the version in `go.mod` (currently **go 1.26.2**; 1.26.3
is fine), `git`, `make`. macOS/Linux. No pre-built binaries.

### Fresh clone

```bash
git clone https://github.com/mmaghsoodnia/gogcli.git
cd gogcli
git remote add upstream https://github.com/openclaw/gogcli.git   # optional, for syncing
make                 # builds bin/gog
./bin/gog --version
```

### Existing checkout on another machine — IMPORTANT

The v0.23.0 sync **rewrote `main`'s history** (force-push). A machine that still
has the old `main` has diverged and cannot fast-forward. Reset it to match:

```bash
git fetch origin
git checkout main
git reset --hard origin/main      # discards old local main; back up first if it has unpushed work
./bin/gog --version
```

After reset, `git rev-parse HEAD` must equal `cf31fe12…` (or whatever this file
records as HEAD above) for an identical build.

### Verify you have the same tree

```bash
git rev-parse HEAD                       # compare to "Current state" above
git log --oneline upstream/main..main    # should show exactly the 3 local patches
go build ./... && go test ./...          # builds clean, full suite green
```

---

## Build / test / lint commands

| Command | Does |
|---|---|
| `make` / `make build` | build `bin/gog` (with version ldflags) |
| `make test` | `go test ./...` (vet off; lint covers vet) |
| `make lint` | golangci-lint (installs pinned tools to `.tools/`) |
| `make fmt` | goimports (local prefix `github.com/steipete/gogcli`) + gofumpt |
| `make ci` | full local gate: fmt-check + lint + test + docs-check |
| `make tools` | install pinned dev tools into `.tools/` |
| `go build ./cmd/gog` | plain build without ldflags |

Pinned tool versions live in the `Makefile` (`TOOLS_VERSION`): gofumpt v0.9.2,
goimports v0.44.0, golangci-lint v2.11.4.

Integration tests are build-tagged and local-only:
```bash
GOG_IT_ACCOUNT=you@gmail.com go test -tags=integration ./internal/integration
```
They require OAuth client credentials + a stored refresh token in the keyring.

---

## Auth, accounts & secrets

`gog` talks to Google via OAuth. Setup (see README "Quick Start" / `docs/`):

```bash
gog auth credentials ~/Downloads/client_secret_....json   # store OAuth client JSON
gog auth add you@gmail.com --services gmail,calendar,drive,docs,sheets,contacts
gog auth doctor --check
export GOG_ACCOUNT=you@gmail.com
```

**Secrets handling (mhive rules):**
- All credentials live in **1Password** — never in the repo, env files, or shell
  history. Never commit OAuth client JSON or tokens (`.env`, `.tools/`, tokens
  are gitignored).
- Tokens are stored in the OS keyring by default. For headless/Docker, use
  `GOG_KEYRING_BACKEND=file` + `GOG_KEYRING_PASSWORD` (inject the password from
  1Password, don't hardcode it).
- This is set up per-machine: each machine needs its own `gog auth add`. The
  repo build is identical across machines; the **auth state is not in the repo**
  and must be configured locally on each.

Useful env vars (full list in README): `GOG_ACCOUNT` (default account),
`GOG_JSON`/`GOG_PLAIN` (output), `GOG_GMAIL_NO_SEND` (hard-block sends),
`GOG_ENABLE_COMMANDS`/`GOG_DISABLE_COMMANDS` (command gating),
`GOG_KEYRING_BACKEND`/`GOG_KEYRING_PASSWORD` (headless keyring).

---

## Source layout

- `cmd/gog/` — CLI entrypoint. `cmd/bake-safety-profile/` — safety-profile baker.
- `internal/cmd/` — all subcommands (gmail, calendar, drive, docs, sheets, …).
- `internal/` — supporting packages: `googleapi`, `googleauth`, `oauthclient`,
  `authclient`, `config`, `secrets`, `outfmt`, `ui`, `safetyprofile`,
  `tracking`, `backup`, `integration` (build-tagged live tests), etc.
- `docs/` — generated command reference + feature/workflow guides.
- `scripts/` — release + docs-site helpers (`scripts/gog.mjs`, gen-command-reference).
- `testdata/` — fixtures.

Output convention: stdout stays machine-parseable (`--json`/`--plain`); human
hints and progress go to stderr.

---

## Staying in sync with upstream

We rebase our small patch set onto new upstream releases rather than merging.
The workflow used for the v0.23.0 sync:

1. `git fetch upstream`
2. Back up: `git branch backup/main-pre-sync-<date> main`
3. Branch from `upstream/main`, then **cherry-pick only the local patches that
   are still unique** (drop any that upstream has since implemented — check with
   `git grep`/`git log` before keeping a feature commit).
4. Resolve conflicts, `go build ./... && go test ./...`.
5. Reset `main` to the synced branch, `git push --force-with-lease origin main`.
6. Tell other machines to `git reset --hard origin/main` (history was rewritten).

Before keeping any feature patch, confirm upstream hasn't already built it —
that's how the rich-formatting commit got dropped.

---

## Known gotchas

- **History gets rewritten on each upstream sync.** Other machines must hard-reset
  `main` to `origin/main` after a sync, not merge.
- **Module path is `steipete/gogcli`, not our fork's path.** Don't "fix" it.
- **Auth is per-machine and not in the repo.** A clean build won't talk to Google
  until `gog auth credentials` + `gog auth add` are run locally.
- **External + Testing OAuth apps** expire refresh tokens after 7 days — publish
  the OAuth app for long-lived tokens.

---

## Per-agent instruction files

- `CLAUDE.md` — Claude-specific behavior (short stub → this file).
- `AGENTS.md` — upstream's generic contributor guide (build/test/style/PR flow).
  Useful reference; not mhive-specific.
- `mhivereadme.md` (this file) — all project knowledge.
- `TODO.md` — work history across sessions.
