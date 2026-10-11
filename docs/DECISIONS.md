# Decisions

Short records of choices that would otherwise get re-litigated. Each says what
was decided and why, in a few lines; the PR that landed it has the detail.
Append as they are made; amend rather than delete when one is reversed. Code
comments cite these by number, so numbers are never reused or renumbered.

Decisions 1–18 were made when the project was bootstrapped, before any feature
work. The ones marked *(to confirm)* name a fact the first task to depend on
them must check, and amend here if it is wrong.

---

### 1. Go, with go-gui for the interface

A native GUI with no browser runtime, written entirely in Go. go-gui renders on
the GPU (Metal on macOS, OpenGL via EGL on Linux and Windows), is event-driven
so the window costs no CPU while idle, and ships a headless test window
(`gui.NewTestWindow`) that drives real layout and event dispatch without a
display, which is what makes views testable in the nightly loop.

### 2. One pinned toolchain, everywhere

`go.mod`'s `go` line is the minimum Go (1.26, go-gui's floor); its `toolchain`
line is the exact Go that `make check` and CI use. golangci-lint and govulncheck
are pinned in the `Makefile` and run through `go run`, so a clean `make check`
on a laptop means a clean CI run.

### 3. macOS and Linux first; Windows after the MVP

The maintainer plays Arena on macOS, and Linux is where development and CI
happen. CI tests on real macOS, because its Metal backend is the one path that
needs cgo, and a macOS build cross-compiled from Linux gets go-gui's stub
backend, which panics at launch. Windows is cgo-free and `make cross` compiles it
on every PR so it does not rot, but it is not tested or shipped until after the
MVP. go-gui is pre-1.0 and changes quickly, so dependabot keeps its bumps out of
the grouped weekly update; each one is reviewed like a feature.

### 4. No panics, no exits, no unsafe, by lint

`panic`, `log.Fatal*`/`log.Panic*`, and `os.Exit` outside `cmd/` are forbidden by
`forbidigo`; `unsafe` by `depguard`. A panic on any goroutine ends the process,
and with it the user's view of their draft. Every goroutine the app starts
recovers and posts the panic to the UI as an error. Tests are exempt.

### 5. Computation, I/O, and rendering are separate layers

Pure functions for parsing, joining, ranking, and state transitions; narrow I/O
packages that only fetch; views that only lay out. Only `internal/ui/run.go`
imports go-gui's `backend` (enforced by `depguard`), so every other package,
views included, runs headless in tests.

### 6. State changes only on the UI thread, by message

go-gui's `*Window` is main-thread-only. I/O goroutines never touch state:
they send an `app.Msg` through `ui.Post`, which uses `Window.QueueCommand` to
apply it with `app.Model.Apply` at the start of the next frame. Apply is a pure
function, so every transition is tested by feeding it messages.

### 7. All colour goes through `ui.Theme`; dark by default

One file holds the palette and type scale, so contrast has one place to check.
Dark is the default because Arena is dark, and a white window flashing beside
it is the first thing a user would close. A light theme, and following the OS
setting, are a later choice.

### 8. One reviewable pull request per night

Inherited from eks-wrangler, with its lessons. There is no line target, because
targets there made the loop split work by size instead of at a seam; a PR is
sized by being a complete change. See `CLAUDE.md`.

### 9. Our own HTTP client: named, rate limited, cancellable, cache-first

Every request carries `version.UserAgent()` (Scryfall requires a named
application; any service owner deserves to know whose traffic it is), goes
through a per-host rate limiter, takes a context so the UI can cancel it, and is
made only after the local cache misses. Re-validation uses `ETag`/
`If-Modified-Since` where the server offers them. `net/http`'s default client is
forbidden by lint. Hosts and their budgets are listed in
`docs/ARCHITECTURE.md`.

### 10. Budgets are checked, not just stated

The stripped release binary must stay under `SIZE_BUDGET_MIB` (28 MiB at
bootstrap, measured at 22.0 MiB with go-gui alone); `make size` enforces it.
CI's smoke job launches the real window under Xvfb and fails if it uses more
than 2% of a core while idle (it measured 0 at bootstrap). Raising a budget is
allowed in the PR that spends it, and has to say what bought the space.

### 11. Pure-Go SQLite for the offline store, added with its first use

`modernc.org/sqlite`: real queries and migrations with no cgo, so Linux and
Windows stay cgo-free. It costs roughly 5–8 MiB of binary, which the first task
that needs persistence pays for by raising the size budget. `mattn/go-sqlite3`
is forbidden by lint because it needs cgo. As with eks-wrangler's async runtime,
the dependency arrives with the first code that uses it, not in the skeleton.

### 12. Card images go through our own store, never go-gui's URL loader

Given an `http(s)` URL, go-gui's `Image` downloads it to `os.TempDir()` with no
size cap and no eviction. We download images ourselves (decision 9), keep them
in the cache directory under a size cap with least-recently-used eviction, and
pass go-gui only local paths. We fetch Scryfall's `normal` size (488×680 JPEG),
which is sharp at the width a companion window shows at 2× scale. We only fetch
images for cards that are actually shown.

### 13. Ratings come from 17lands, with a user-chosen tier list for grades

Data-driven stats (games-in-hand win rate, average last seen, and so on) come
from 17lands per set and format. Letter grades, such as LSV's, come from a 17lands
tier list the user picks, rather than from scraping articles, which breaks when
the page changes and may not be allowed by the publisher. The ratings sit behind
a `Source` interface so another provider can be added later. The UI always says
which source a number came from. *(to confirm: 17lands' current endpoints,
response fields, and terms of use, before the first fetch ships)*

### 14. The log is followed by polling, not file-system events

`Player.log` is read from the last offset on a short poll (a `stat` and, only if
the file grew, one read). This needs no dependency, behaves the same on macOS,
Linux, and through Wine/Proton prefixes, and makes truncation (Arena starts a
fresh log on every launch) a size check. A poll of a few hundred milliseconds
costs nothing measurable and is well inside a human's pick time.

### 15. Cards are identified by Arena's id; Scryfall supplies names and images

The log names cards by Arena's numeric id (`grpId`). Scryfall's card objects
carry `arena_id`, so a per-set search (a few paged requests, not the bulk file)
maps every id in a set to a name, a collector number, and image URLs. Where
Scryfall does not know an id yet, which happens with brand-new and digital-only
cards, the card shows by id with a "not on Scryfall yet" note rather than not at
all. Reading Arena's own local card database is a possible fallback, left as a
roadmap decision. *(to confirm: Scryfall's coverage of `arena_id` for the current
draft sets)*

### 16. Cache directory for everything; read-only toward Arena

The database and images live under `os.UserCacheDir()/mtga-draft-assistant`,
because everything there can be fetched again. Deleting it is always safe and
the README says so. Settings, once there are any, go under `os.UserConfigDir()`.
We never write to Arena's directories.

### 17. No telemetry, no update checks, no accounts

The only network traffic is for card data, images, and ratings, as listed in the
network budget. Releases are announced on GitHub; the app does not phone home.

### 18. A companion window, not an overlay

A normal window placed beside Arena, optionally always on top, works the same
on every platform and does not need to know where Arena draws its cards at each
resolution. An overlay drawn over the pack is a roadmap idea, and a decision for
the maintainer, not an assumption built into the code.

### 19. Time to first frame is measured by the app, on request, and reported

`-startup-time` prints one line to stderr once the first frame has been
presented: the total from process start, against the 250 ms target, and the
time spent in each stage (Go runtime and init, window and GPU context, first
view, then layout, accessibility bridge, draw and present). Off, the window is
built exactly as without it. Process start comes from the OS (`/proc` on
Linux, good to 10 ms; `sysctl` on macOS); elsewhere the count starts at
`main`, and the line says so. go-gui has no "frame presented" callback, so the
first view's build queues a command, and go-gui runs it at the start of the
next frame, after the first was presented. `make smoke` (Linux, Xvfb) and
CI's macOS test job print it on every PR. It is reported, not gated: a fresh
runner always has a cold shader cache, which a user's second launch never
does, so CI's number overstates what users see. Any gate is the maintainer's
call.
