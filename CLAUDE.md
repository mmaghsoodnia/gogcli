# CLAUDE.md

Read `mhivereadme.md` first — it is the single source of truth for this project
(architecture, build, the fork/upstream-sync state, and how to reproduce the
build on another machine). Check `TODO.md` for in-flight work.

This is a **fork** of `openclaw/gogcli`. `origin` is our fork, `upstream` is the
source. The Go module path stays `github.com/steipete/gogcli` — do not rename it.
Secrets live in 1Password and are never committed; auth is configured per-machine.
