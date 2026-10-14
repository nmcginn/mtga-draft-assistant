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
                       poll (decision 22). Parsing drafts lands here next.
  background/          Every goroutine the app starts, through one helper that
                       recovers a panic and posts it to the window (decision
                       4); and the tasks themselves, which call the I/O
                       packages and turn what they report into app messages.
  startup/             Time to first frame: when each stage of startup
                       finished, and the one-line report -startup-time prints
                       (decision 19).
  version/             The build's version and the User-Agent every request
                       carries.
```

Planned packages, added by the roadmap task that first needs each one (and
added to this map by the same PR):

```
  draft/               Picks, packs, pools: the pure draft model and the
                       ranking of a pack against ratings.
  store/               The SQLite cache: schema, migrations, reads and writes
                       of sets, cards, and ratings.
  fetch/               The app's HTTP client (User-Agent, per-host rate limit,
                       context, conditional requests), and one sub-package per
                       source: scryfall/, seventeenlands/.
  images/              The on-disk image cache: size cap, LRU eviction, and
                       local paths handed to go-gui.
```

The binary is a shell around `internal/`. If a behaviour only exists in
`main.go`, it cannot be tested — so it does not go there.

## The rule: separate computation, I/O, and rendering

Three layers, and the boundaries are load-bearing:

**Computation** — parsing log lines, joining cards to ratings, ranking a pack,
every state transition. Pure functions over plain data: `app.Model.Apply`,
`app.Model.Status`. All of it is tested directly.

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
  (`first-paint.png`, `following.png`) as the `first-paint` artifact.

## Budgets

These are the numbers that "good citizen" means here. A change that moves one
updates this section in the same PR.

### Network

Every request is made only after the cache misses, carries the User-Agent,
waits on a per-host rate limiter, and can be cancelled (decision 9).

| Host | What | When | Ceiling |
| ---- | ---- | ---- | ------- |
| `api.scryfall.com` | Cards in a set, by `arena_id` (paged search) | First time a set is seen; then at most weekly | 1 request / 100 ms; a handful of pages per set |
| `cards.scryfall.io` | Card images, `normal` size | First time a card is shown | 4 concurrent; ~70 KB each |
| `17lands.com` | Ratings for a set and format | First draft of a set; then at most once per 24 h | 1 request per set+format per day |
| `17lands.com` | The user's chosen tier list | When chosen; then at most once per 24 h | 1 request per list per day |

Scryfall's bulk-data files are never downloaded: they hold every card ever
printed, and we need one set at a time. A repeat draft of a set we already have
costs no requests at all beyond the daily ratings check.

### Disk

Everything lives under `os.UserCacheDir()/mtga-draft-assistant` (decision 16):

- `cache.db` — sets, cards, ratings. Expected to stay in the low megabytes.
- `images/` — capped at 200 MB by default, with least-recently-used eviction
  (decision 12). That holds a few thousand cards, so several sets.

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
  error), never per poll. The smoke job's second launch follows a fixture
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
