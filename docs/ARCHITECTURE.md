# Architecture

## Shape

```
cmd/mtga-draft-assistant/
  main.go              Flags, then ui.Run. Nothing else.
internal/
  app/                 The whole application state (Model), the messages that
                       change it (Msg), and Apply: pure, and tested by feeding
                       it messages.
  ui/                  go-gui views over an app.Model, the Theme, ui.Post for
                       handing messages to the UI thread, and run.go: the one
                       file that opens a real window.
  arena/               Arena's log: where it may be on each platform (pure),
                       the search that looks there until it turns up, and
                       the follower that reads it as it grows: a pure Cursor
                       that turns bytes into whole lines, around a thin
                       poll (decision 22); and the Parser that turns those
                       lines into draft events (pure; Premier and Quick
                       Draft, decision 23).
  draft/               The draft under way as plain data: which draft, the
                       pack on show, the picks, the pool; and Apply, which
                       folds the parser's events into it (pure, decision 24).
                       The ranking of a pack against ratings joins it with
                       the ratings task.
  background/          Every goroutine the app starts, through one helper that
                       recovers a panic and posts it to the window (decision
                       4); and the tasks themselves, which call the I/O
                       packages and turn what they report into app messages:
                       the log task runs the parser and posts each batch's
                       draft events as one message, and tells the card task
                       the set of each draft it reads the start of; the card
                       task reads that set from the store, or fetches it from
                       Scryfall and stores it, and retries when offline
                       (decision 26); the image task is told by the window
                       which pictures are on screen, and hands back each
                       one's local path, from disk or fetched four at a
                       time, retrying failures on their own schedule
                       (decision 27).
  cards/               A card's details as plain data (name, mana cost, type,
                       rarity, picture address), keyed by Arena id, and how they read at a
                       glance: "2GG · Creature — Elf Druid · Uncommon".
  store/               The SQLite cache (decision 11): schema with
                       migrations, and reads and writes of sets and their
                       cards. Only reads and writes; staleness is the card
                       task's call.
  fetch/               The app's HTTP client (decision 25): User-Agent,
                       per-host rate limit, context, backoff on 429/503,
                       conditional requests. Every request goes through it.
    scryfall/          A set's cards from Scryfall's paged search, by
                       arena_id; and card pictures, downloaded.
  images/              The picture folder: a size cap with least-recently-
                       used eviction that survives restarts, files checked
                       and written whole, and local paths handed to go-gui
                       (decisions 12, 27). Only reads and writes files.
  pool/                A set of drafted cards counted up: colours, mana
                       curve, creatures and other spells, lands (pure,
                       decision 28). The picks panel shows it.
  startup/             Time to first frame: when each stage of startup
                       finished, and the one-line report -startup-time prints
                       (decision 19).
  version/             The build's version and the User-Agent every request
                       carries.
```

Planned packages, added by the roadmap task that first needs each one (and
added to this map by the same PR):

```
  fetch/seventeenlands/
                       Ratings, through the same client.
```

The binary is a shell around `internal/`. If a behaviour only exists in
`main.go`, it cannot be tested — so it does not go there.

## The rule: separate computation, I/O, and rendering

Three layers, and the boundaries are load-bearing:

**Computation** — parsing log lines, joining cards to ratings, ranking a pack,
every state transition. Pure functions over plain data: `app.Model.Apply`,
`app.Model.Status`, `app.Model.DraftScreen`, `draft.Apply`, `pool.Summarize`. All of it is
tested directly.

**I/O** — the log file, the cache database, the network. Confined to narrow
functions that do nothing but read or fetch and hand back data. They run on
goroutines, never on the UI thread, and report what they found as an `app.Msg`.

**Rendering** — `ui.mainView` and the views below it. Takes the state and lays it
out. It makes no decisions that are not about layout, and it never fetches
anything: an image that is not on disk yet is a placeholder plus a message to
the image fetcher, not a download in the view.

## Data flow

```
                   ┌───────────── goroutines (I/O) ──────────────┐
  Player.log ──▶ follower ──lines──▶ parser ──events─┐            │
                                                     ▼            │
  Scryfall ◀──▶ fetch ◀──▶ store (SQLite) ──cards/ratings──▶ ui.Post(msg)
  17lands  ◀──▶   │                                              │
  card art ◀──────┴──▶ images (disk, capped) ──local path───────▶│
                   └──────────────────────────────────────────────┘
                                                     │ Window.QueueCommand
                                                     ▼
                     UI thread: model = model.Apply(msg) → mainView(model)
```

One state slot per window holds the `app.Model` and the `ui.Theme`. Only the
UI thread reads or writes it (decision 6).

## Testing seams

- **State:** construct an `app.Model`, apply messages, assert on fields and on
  `Status()`.
- **Views:** `gui.NewTestWindow(t, windowCfg(model))`, then `TestRender`, then
  walk the layout tree for `Shape.TC.Text`, IDs, and colours. To exercise
  `ui.Post`, call `w.FrameFn()`, which drains the command queue the way a real
  frame does. `TestClick`, `TestKey`, and `TestScroll` drive input. Headless text
  measurement is approximate, so never assert on pixel widths.
- **Log parsing:** fixtures in `testdata/`, cut from real logs and scrubbed
  (`testdata/README.md`).
- **Network:** `httptest.Server` serving recorded responses; the client under
  test is pointed at it. Tests never reach the real services.
- **The real window:** `make smoke` under `xvfb-run` launches the release
  binary twice, plainly and with `-log` naming a fixture, screenshots each
  once it settles, and checks idle CPU in both. CI uploads the screenshots
  (`first-paint.png`, and `following.png`, which shows the fixture's finished
  draft) as the `first-paint` artifact. Each launch gets an empty cache of
  its own and a proxy that refuses every connection, so the fixture's draft
  shows the offline message rather than reaching Scryfall from CI.

## Budgets

These are the numbers that "good citizen" means here. A change that moves one
updates this section in the same PR.

### Network

Every request is made only after the cache misses, carries the User-Agent,
waits on a per-host rate limiter, and can be cancelled (decision 9). A 429 or
503 is retried up to three times, waiting as long as `Retry-After` says (up
to a minute) or 1 s doubling, and every request to that host waits it out
too (decision 25). A host with no row here gets one request a second.

| Host | What | When | Ceiling |
| ---- | ---- | ---- | ------- |
| `api.scryfall.com` | Cards in a set, by `arena_id` (`/cards/search`, `e:<set> game:arena`, every print) | First draft of a set; then at most weekly, or daily for a set Scryfall had no cards for | 1 request / 500 ms; a handful of pages per set, at most 20; offline, retried after 30 s doubling to every 5 min while that set is on screen |
| `cards.scryfall.io` | Card images, `normal` size (decision 27) | First time a card is shown in a pack, then never again while it is on disk | 4 concurrent, 1 request / 100 ms; ~70 KB each, a pack ~1 MB; a failed picture retried after 30 s doubling to every 5 min while it is on screen |
| `17lands.com` | Ratings for a set and format | First draft of a set; then at most once per 24 h | 1 request per set+format per day |
| `17lands.com` | The user's chosen tier list | When chosen; then at most once per 24 h | 1 request per list per day |

Scryfall's bulk-data files are never downloaded: they hold every card ever
printed, and we need one set at a time. A repeat draft of a set we already have
costs no requests at all beyond the daily ratings check.

### Disk

Everything lives under `os.UserCacheDir()/mtga-draft-assistant` (decision 16):

- `cache.db` — sets and their cards, with each card's picture address
  (ratings to come). A set is a few hundred
  rows of short text, tens of kilobytes. Opened only once a draft needs a
  set. If it cannot be created or read (a folder that cannot be written, a
  corrupt file, one from a newer version) the assistant works from memory,
  fetches each launch, and says so under the draft.
- `images/` — capped at 200 MB, with least-recently-used eviction (decisions
  12, 27). That holds a few thousand cards, so several sets. The last use is
  each file's modification time, so the order and the cap hold across
  restarts; pictures on screen are never evicted. Created only once a pack
  with pictures is shown. If it cannot be written, cards show by name, the
  window says so under the draft, and it is tried again from 30 s, doubling
  to every 5 min.

### CPU, memory, and size

- **Idle:** no measurable CPU while nothing changes. The smoke job fails above
  2% of a core over five idle seconds (decision 10). go-gui only redraws when
  something is invalidated; the log follower's poll is a `stat`. While no log
  has been found, the search looks every 3 s: one glob per place (seven on
  Linux, one on macOS), and it tells the window only when the answer changes,
  so waiting for Arena never redraws (decision 20). The smoke job runs in this
  state and measures 0 ticks. Once the log is found, the follower stats it
  every 200 ms and opens it only when it grew (decision 22); it posts to the
  window only when something changes (caught up, a new session, a read
  error, draft events in what was read), never per poll. Draft events go
  as one message per batch read, so catching up on a session redraws once
  per megabyte, not once per pick (decision 24). The smoke job's second launch follows a fixture
  and measures 0–1 ticks over 5 s, ~6 over 30 s (0.2% of a core, under Xvfb);
  the cost is the poll itself and scales with its rate (1 tick over 30 s at
  1 s, 24 at 50 ms).
- **Following the log:** reads only the bytes appended since the last poll,
  at most 1 MiB at a time, so catching up on a long session holds a megabyte,
  not the log. A line longer than 16 MiB is dropped rather than buffered. The
  file is opened only for each read and closed again.
- **Startup:** window up within 250 ms of launch; first paint never waits on
  disk beyond what the first frame shows, or on the network. Measured with
  `-startup-time` (decision 19); `make smoke` and CI's macOS job print it.
  Under Xvfb with Mesa's software renderer it is ~150–220 ms with a warm
  shader cache and ~370–400 ms with a cold one, most of the difference in
  compiling shaders for the first draw. go-gui connects its accessibility
  bridge (D-Bus on Linux) between building the first frame and presenting
  it: ~2 ms with the desktop's AT-SPI bus already running, ~30 ms when the
  first connection starts it, ~0.1 s when there is no session bus at all.
- **Binary:** under `SIZE_BUDGET_MIB` (`Makefile`), enforced by `make size`.
  26.3 MiB with SQLite (decision 11): `modernc.org/sqlite` costs 4.1 MiB,
  measured against the same program without it; `net/http`'s client almost
  nothing more, since go-gui already linked TLS. 26.4 MiB with card images
  (decision 27): their decoders were already linked by go-gui.
- **Memory:** to be budgeted once real numbers exist. Xvfb with Mesa's software
  renderer reports ~150 MB RSS for the empty window, which says more about
  llvmpipe than about the app; measure on real hardware first.

## Platform notes

### Where Arena writes its log

Arena writes `Player.log` while it runs, and moves the previous session's log
to `Player-prev.log` on the next launch. Draft events only appear with
**Options → Account → Detailed Logs (Plugin Support)** turned on.

| Platform | Location |
| -------- | -------- |
| macOS | `~/Library/Logs/Wizards Of The Coast/MTGA/Player.log` |
| Windows | `%USERPROFILE%\AppData\LocalLow\Wizards Of The Coast\MTGA\Player.log` |
| Linux (Wine, Lutris, Proton) | The Windows path inside the prefix: `<prefix>/drive_c/users/<user>/AppData/LocalLow/Wizards Of The Coast/MTGA/Player.log` |

Arena's Steam app id is **2141910**, so Proton's prefix is
`steamapps/compatdata/2141910/pfx`, and its user is `steamuser`. The search
(`arena.Candidates`, decision 20) looks in, on Linux and in this order:
`$WINEPREFIX`, `~/.wine`, every Lutris prefix under `~/Games`, every bottle
under Bottles' data directory (native and Flatpak), and the Proton prefix under
each of `~/.steam/steam`, `$XDG_DATA_HOME/Steam` (default
`~/.local/share/Steam`) and Flatpak Steam's `~/.var/app/com.valvesoftware.Steam`.
Inside a prefix any user name matches. Anywhere else, `-log <path>` names the
file (or the folder holding it).

### Building

- **Linux:** `CGO_ENABLED=0`. At runtime go-gui loads `libEGL.so.1` with
  purego and talks to X11 (Wayland is experimental upstream:
  `GOGUI_WAYLAND=1`).
- **macOS:** needs cgo and the Xcode command-line tools for go-gui's Metal
  backend. Cross-compiling for macOS from Linux produces a binary that panics at
  launch (decision 3), so macOS is built on macOS.
- **Windows:** `CGO_ENABLED=0`; compile-checked by `make cross`, not shipped
  until after the MVP.
