# TODO / work history

## Done (2026-06-10)
- [x] Sync fork with upstream. Fork was 469 commits / 11 versions behind
      (v0.12.0 → v0.23.0). Rebased our local patches onto `upstream/main`:
  - Kept: `webContentLink` in drive upload (`e8b859e6`), `docs comments list
    --show-anchors` (`3394fa3a`), gitignore linux binary (`cf31fe12`).
  - Dropped: rich `docs format` commit — superseded by upstream's own `docs
    format`. Preserved on `backup/main-pre-sync-20260610`.
  - `main` now at `cf31fe12` (`v0.23.0-16`); builds clean, full test suite green;
    force-pushed to `origin`.
- [x] Migrate to mhive doc convention: created `mhivereadme.md` (project SOT incl.
      fork/sync state + reproducible-build instructions for other machines),
      `TODO.md`, and `CLAUDE.md` stub.

## Follow-ups (optional)
- [ ] Drop `backup/main-pre-sync-20260610` once the v0.23.0 sync is confirmed good
      on all machines (`git branch -D backup/main-pre-sync-20260610`).
- [ ] Other machines: `git fetch origin && git reset --hard origin/main` (history
      was rewritten by the sync — they cannot fast-forward).
