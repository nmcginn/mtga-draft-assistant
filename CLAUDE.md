# mtga-draft-assistant — working agreement

`mtga-draft-assistant` is a desktop companion for MTG Arena drafts. It reads the
log Arena writes as you play, works out which pack is in front of you, and shows
that pack ranked by rating, with card images, beside the game. Later it helps
build the deck from what you drafted.

People glance at it mid-pick with a timer running. It has to be the one window
that is never in the way: quiet on the machine, light on the network,
instant to react, and readable at a glance.

Most work here is done by Claude, one pull request per night, reviewed by a human
in the morning. That cadence shapes everything below: each change must stand on
its own and be verifiable without the reviewer launching Arena.

## Priorities

In this order, when they conflict:

1. **Be a good citizen.** On the user's machine and on the services we lean on.
   - *Network:* never fetch what we already have. Every request is cached, named
     (our User-Agent), rate limited per host, and cancellable. Card data and
     images are fetched once and kept; ratings are refreshed at most once a day
     per set. No bulk downloads, no telemetry, no update checks. Each source has
     a written budget in `docs/ARCHITECTURE.md`, and a new request type updates
     it.
   - *Disk:* everything we write lives under the OS cache directory, the image
     cache has a size cap with eviction, and deleting the cache directory is
     always safe. We only ever *read* Arena's files.
   - *CPU and memory:* zero CPU when nothing is happening (the smoke job checks
     it). Following the log reads only the bytes that are new.
   - *Size:* the release binary has a size budget (`SIZE_BUDGET_MIB` in the
     `Makefile`). A dependency must earn its megabytes.
2. **Responsiveness.** The UI thread never waits on I/O. Disk, network, and log
   reading happen on goroutines that post messages back; the window paints from
   whatever state it has, with a visible loading state for anything still
   coming. First paint never waits on the network or on reading the whole log.
   Target: the window is up within **250 ms** of launch, and a new pack is on
   screen within **one frame** of its line reaching the log.
3. **Information that is easy to take in.** Someone has seconds to pick. The
   best card in the pack is obvious without reading; ratings say where they came
   from; nothing is shown that does not help the pick. Card names, not Arena
   IDs. Errors say what to do next ("turn on Detailed Logs in Arena's Account
   settings"), never just what broke.
4. **Testing and validation.** Logic is tested without Arena, without a display,
   and without the network. Anything that can be a pure function should be,
   precisely so it can be tested.

## Architecture

`cmd/mtga-draft-assistant/main.go` is a thin shell; everything real lives under
`internal/` so it can be tested. See `docs/ARCHITECTURE.md` for the package map
and budgets, and `docs/DECISIONS.md` for why things are the way they are.

The rule that matters most: **separate computation from I/O and from rendering.**

- Parsing log lines, joining cards to ratings, ranking a pack, and every state
  transition are pure functions over plain data.
- I/O (following the log, the cache database, HTTP) lives in narrow packages
  that do nothing but fetch and hand back data, and run off the UI thread.
- The UI thread changes state only by applying messages: `app.Model.Apply(msg)`,
  reached through `ui.Post`. Views read state and lay it out; they never fetch.
- Only `internal/ui/run.go` opens a real window. Every view is tested headless
  with go-gui's `gui.NewTestWindow`; the lint enforces this.

## Testing standards

- Every bug fix starts with a failing test.
- Test behaviour, not implementation. Test names are sentences describing the
  guarantee: `TestAPackSeenTwiceIsRankedOnce`, not `TestParse2`.
- Views are tested through `gui.NewTestWindow` and `TestRender`: assert on the
  text that lands in the layout tree, and on IDs and theme colours, never on
  pixel positions (text measurement is approximate headless).
- Log parsing is tested against fixtures under `testdata/` cut from real
  `Player.log` files and scrubbed of account details (see `testdata/README.md`).
  Never invent a log format from memory: if no fixture shows it, the task
  needs one first.
- Network clients are tested against `httptest` servers serving recorded
  responses. Never write a test that touches the real Scryfall or 17lands.
- Always include the awkward cases: an empty log, a line cut off mid-write, a log
  that is truncated because Arena restarted, a card with no rating, a set we have
  never seen, a 1x1 window, a cache directory that is not writable.
- `make check` must pass before anything is pushed. CI runs the same thing, plus
  a real launch under Xvfb that screenshots first paint and fails if the app is
  not idle.

## Conventions

- **Go**, at the toolchain pinned in `go.mod`. Tools (golangci-lint,
  govulncheck) are pinned in the `Makefile` and run through `go run`, so local
  and CI results match.
- **No panics outside tests.** `panic`, `log.Fatal`, and `os.Exit` outside `cmd/`
  are denied by lint. A panic on *any* goroutine kills the process, so every
  goroutine the app starts recovers and reports a panic as an error message to
  the UI. Return errors.
- **No `unsafe`.** Denied by lint. **No cgo** outside go-gui's macOS backend:
  Linux and Windows build with `CGO_ENABLED=0`.
- **Errors:** wrap with `fmt.Errorf("...: %w", err)` and compare with
  `errors.Is`/`errors.As`. Text shown to the user is written for someone
  mid-draft: what failed, and what to do about it.
- **All colour goes through `ui.Theme`.** Never a `gui.Hex`/`gui.RGB` in a view.
- **Every outbound request goes through the app's HTTP client**, which sets the
  User-Agent, rate limits per host, and takes a context. `net/http`'s default
  client is denied by lint.
- Comments explain *why*. The code already says what.
- Add a dependency only when it earns its place; record it in `docs/DECISIONS.md`
  with what it costs in binary size.

## The nightly loop

Each night a fresh session picks up the next task. The procedure is in
`.claude/commands/nightly.md` — run `/nightly`. In short:

1. Read `docs/ROADMAP.md` and take the highest-priority unchecked task that fits
   in **one reviewable pull request**.
2. Branch from `master` as `nightly/<yyyy-mm-dd>-<slug>`.
3. Build it, with tests. Run `make check`.
4. Open a PR that explains what changed, how it was verified, and what a
   reviewer should look at. Tick the task in `docs/ROADMAP.md` in the same PR.

### What "one pull request" means

One complete change, not one line count. There is no line target — size is not
the test. The reviewer should be able to read it over coffee and see a whole
thought — so the first question is "is this finished?", and only then "is this
small?".

A PR carries whatever it takes to be complete: the change, its tests, fixtures,
and docs. Tests here run two to four times the length of the code they cover,
and a change is never worth splitting because its tests are long — and it is
equally not worth splitting because the production diff itself ran long.

Splitting is a claim, and a follow-up has to earn it. One of these must be true:

- it lands on a surface this PR does not touch — another view, another data
  source, a package that does not exist yet;
- it turns on a decision that is the reviewer's to make, so building it now would
  be guessing at the answer;
- it needs something only the human can provide — a fixture from a real Arena
  session, a judgement about a third party's terms;
- it is a night's work on its own.

Finishing what you just built is none of those. If a follow-up exists only
because the change stops short of the priorities above — a message that
diagnoses without advising, a request that is cached on one path and not its
twin, a loading state on one view and not the one beside it — it is not a
follow-up. It is the rest of the task, and it ships now, however large that
makes the diff. Two thirds of a change tonight and the last third tomorrow costs
the reviewer two readings of the same paragraph.

The tell is in your own writing: when the review notes say "the cost is that…",
what follows is usually the half you should have finished.

Never leave `master` broken, never merge your own PR, and never weaken a test to
get CI green — if a test is wrong, fix the test deliberately and say so in the PR.
