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

*Measured when it arrived:* 4.1 MiB, not 5–8, taking the release binary from
22.2 to 26.3 MiB, inside the 28 MiB budget, which was left where it is. The
database is opened by URI (`file:` with the path escaped), because the driver
cuts a plain path at its first `?`.

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

*Not yet confirmed:* the session that built the Scryfall client could not
reach Scryfall, so its tests run on pages written to Scryfall's documented
schema rather than recorded (`testdata/scryfall/README.md`). Every card id in
both FRA fixtures' packs falls in one run (106225–106535), which is what one
set searched by code would cover.

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

### 20. Finding the log: glob the usual places, newest wins, say where we looked

`arena.Candidates` is a pure function from platform, home, and environment to
glob patterns, one per place Arena is commonly installed; a pattern's wildcards
stand for what varies (a Lutris game folder, a bottle's name, the user inside a
prefix) and everything else is escaped. When more than one log exists, the one
written most recently is used, since that is the install in use. The search
looks every 3 s until it finds one, and posts only twice at most: where it looked,
then what it found. `-log` is checked before the window opens; a bad path is an
error on stderr and exit status 2, never a quiet search, and a folder holding a
`Player.log` is taken to mean that file. Steam libraries on other drives, and
launchers beyond Wine, Lutris, Bottles and Steam, are left to `-log` for now.

### 21. Background tasks report failures to the window, once

Every goroutine starts through `background.Go`, which recovers a panic, writes
the stack to stderr for a bug report, and posts `app.TaskFailed`. The window then
says which task stopped and to restart, and links the issue tracker. Only the
first failure is kept: it is the likeliest to explain the rest, and the window
stays on it until restart, because the stopped task will not update anything
else it would have shown.

### 22. Following the log: a pure cursor, a 200 ms stat, restart by identity

`arena.Cursor.Consume` is the whole of the line logic, a pure function from
bytes read to whole lines and the next offset; a line with no newline yet is
held back, and `\r\n` is trimmed. The follower around it stats the log every
200 ms (decision 14) and opens it only when it grew, reading at most 1 MiB at a
time and closing it after, since a held handle on Windows would stop Arena
moving its log aside. The log has started over when it is shorter than what was
read, or is no longer the same file (`os.SameFile`: Arena moves `Player.log` to
`Player-prev.log` on launch); it is then read from the start and the window is
told a new session began. A truncation that has already regrown past the old
offset in the same file between two polls cannot be seen this way; that is
accepted, since Arena replaces the file. The log counts as lost only after 3 s
missing, so the moment between Arena moving the old log and creating the new
one does not flash the window back to "Waiting". Lost, the search starts again;
a log named with `-log` is then waited for at that path alone, and the window
says so instead of advising `-log`. A read failure is shown with its reason
and what to do, once per distinct reason, and retried on every poll.

### 23. Draft parsing: line shapes from fixtures, one stateful pure parser

`arena.Parser` reads one line at a time and returns draft events: started, pack
shown, pick made, ended. Its only state is plain data (a response header
awaiting its body on the next line, the draft under way, the last pack shown),
so it does no I/O and is tested by feeding it fixture lines; a new log needs a
new Parser. Each format's lines come from its fixture: Premier Draft's
`Draft.Notify`, `EventPlayerDraftMakePick` and `DraftCompleteDraft`; Quick
Draft's `BotDraftDraftStatus` and `BotDraftDraftPick`, which count packs and
picks from 0 (events count from 1). The format and set come from the event
name (`PremierDraft_FRA_20260929`). A draft joined in a format with no fixture
is skipped until the next join, rather than read as a format it may not be. A
pick is taken from its request, not its confirmation; a pack identical to the
last one shown is reported once. A bot draft's status names its event, so it
starts a draft the log never saw joined (Arena relaunched mid-draft); a draft
against people has no such line in any fixture yet. Skipped draft lines go to
`slog` at debug level, which the default logger drops.

### 24. The draft in the model: one message per batch, Arena's pick list as a set

`draft.Apply` folds parsed events into a `draft.Draft` (which draft, the pack on
show, the picks, the pool); `app.Model` holds it and `Model.DraftScreen` puts it
into words, as `Status` does for the empty states. The log task runs the parser
on its own goroutine and posts one `app.DraftEvents` per batch of lines read, so
catching up on a long session is a frame per megabyte, not one per pick. The
parser and the draft start afresh with each log and each Arena session; a draft
Arena reopens is read again from the new log. A pick at a position already held
replaces it, and a pack from earlier than the one on show is ignored, so
reconnects do not duplicate. A bot draft's packs carry Arena's `PickedCards`,
which the fixture shows sorted by id, not in pick order: cards in it that were
not seen picked are added first, as earlier picks with no position, rather than
placed where they might have been. A pack or pick with no draft started begins
one with no name (Premier Draft resumed, decision 23). A finished draft stays on
screen, with its pool, until the next one starts. A failure or an unreadable log
is shown instead of the draft, which would otherwise look current.

### 25. One HTTP client: a slot per host, backoff that the whole host obeys

`fetch.Client` is decision 9 built. Each host has a least interval between
the starts of two requests (Scryfall 500 ms, five times what it asks for,
since a set is a few pages; any host not written down, 1 s), booked under a
lock so goroutines share it. A 429 or 503 is retried up to three times after
`Retry-After` (seconds or a date; over a minute is not waited on in-request)
or 1 s doubling, and the wait is put on the host, so the next request to it
waits too. Redirects are not followed: they would be requests to a host we
did not choose. Bodies are capped (16 MiB by default). Errors are worded for
the user: `HostError` is "can't reach api.scryfall.com", `StatusError` "…
answered 500 Internal Server Error" or "… is busy". Conditional requests
(`ETag`, `Last-Modified`) are supported for the sources that refresh one
document (ratings); Scryfall's search is several pages that can change
independently, so a stale set is fetched again whole rather than asked about
page by page.

### 26. A set's cards: fetched once, shown at once, retried while offline

The log task tells the card task the set of each draft it reads the start of;
only the latest set asked for is fetched, so catching up on a session with
several drafts fetches the one on screen. The card task opens the cache only
then, so a launch with no draft never touches the disk. A stored set is shown
at once; if it is older than a week (a day, when Scryfall had no cards for
it, as with a set too new for it), it is fetched again quietly, and a
failure then leaves the stored cards on screen. A set not stored is fetched,
saved in one transaction (never half a set), and shown; a fetch that fails on
any page fails whole. Offline, the window says what failed and when it tries
again: 30 s, doubling to every 5 min, for as long as that set is the one
wanted. A cache that cannot be used is reported under the draft with what to
do, and the cards are fetched each launch instead. In the window, a card
with no details is "Card 106325": with "Not on Scryfall yet" under it when its
set is known and it is not in it, or one notice above the pack when the
whole set is still coming, could not be fetched, or was never named by the
log. Scryfall is credited under the draft.
