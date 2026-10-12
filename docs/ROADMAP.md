# Roadmap

The backlog for the nightly loop. Open tasks are ordered by priority: take the
highest unchecked one that fits in a single pull request and is not marked
**needs human**.

Each task lists acceptance criteria. A task is done when they hold, tests cover
them, and `make check` passes. In the same PR, move it to **Done** as one line
naming what landed and its decision numbers. The PR and `docs/DECISIONS.md` hold
the detail, so this file stays short.

If a task is larger than one PR, land the smallest **complete** slice (one a
user meets as a finished change), tick nothing, and split the rest into new
tasks here. Split only at a seam: a surface this PR does not touch, or a
decision the reviewer should make first. An entry that exists only because the
last change stopped short of the bar in `CLAUDE.md` is unfinished work, not a
follow-up, and it belongs in the PR that raised it.

**Needs human** marks work only the maintainer can do: capture a fixture from a
real Arena session, make a call about a third party's terms, or hold an account
(such as Apple's developer programme). The loop skips these and the tasks
blocked on them, and says so in its PR.

---

## Open

### Milestone 1 — Live draft picks (the MVP)

The goal: open the app, start a draft in Arena, and see the current pack ranked
by rating, with card images, before the pick timer matters. macOS and Linux
(decision 3).

- [ ] **Needs human: capture draft log fixtures, one draft at a time.**
  Following `testdata/README.md`, scrubbed. Drafts cost an entry fee, so these
  arrive as the maintainer plays them, not all at once; each lands in its own
  PR, and each one unblocks work below as soon as it is merged.
  - [ ] The start of a `Player.log` from a fresh Arena launch, without a draft.
  - [x] The first draft, in whichever format is played first, from launching
    the event through at least three picks. Unblocks the parsing task.
    Premier Draft, FRA, all 42 picks:
    `testdata/logs/2026-10-fra-premier-full-draft.log`.
  - [x] Quick Draft (bot draft), the same way. Unblocks its format in "Parse
    the remaining draft formats". FRA, all 42 picks:
    `testdata/logs/2026-10-fra-quick-full-draft.log`.
  - [ ] Traditional Draft, the same way. Unblocks its format in "Parse the
    remaining draft formats".

- [ ] **Follow the log as it grows.**
  - Reads only new bytes from the last offset on a short poll (decision 14);
    buffers a trailing partial line until its newline arrives.
  - Detects truncation (the file got smaller, or was replaced, because Arena
    restarted) and starts again from the beginning, posting that a new Arena
    session began.
  - On first finding the log, reads the current session from the start, so a
    draft already in progress is picked up, without blocking first paint.
  - The core (given an offset, a size, and the bytes read, return complete lines
    and the next offset) is a pure function; the polling loop is thin.
  - When the log disappears, post `app.LogLost` and start the search again
    (`background.FindLog`'s start), so the window lists where it is looking.
  - Tests: empty file, partial line completed by a later read, truncation,
    replacement, a multi-megabyte log read incrementally, a file that
    disappears.

- [ ] **Parse draft events from the log.** *Blocked on the first draft
  fixture.*
  - Turns log lines into typed events: a draft started (set, format, event),
    a pack shown (pack and pick number, card ids), a pick made (card id), the
    draft ended (the pool).
  - Covers every draft format that has a fixture in `testdata/logs/` when the
    work starts; one is enough to begin. Arena may log each format
    differently, so a format with no fixture is not guessed at: its lines are
    skipped like any other unknown line, and the PR names the formats still
    to come. Lines that are not draft events are skipped cheaply: most of the
    log is not about drafting.
  - Unknown or malformed draft lines are logged at debug level and skipped,
    never fatal.
  - Tests: every fixture, a line cut off mid-JSON, a pack seen twice (Arena
    re-logs on reconnect), events out of the order expected.

- [ ] **Parse the remaining draft formats.** *Each blocked on its fixture.*
  Whichever of these the parsing task did not already cover, one PR per
  format as its fixture lands: the parser learns that format's lines, and the
  same tests run against its fixture, including a whole draft replayed through
  the model once the next task exists. Tick a format here when it is
  covered, by either task.
  - [ ] Premier Draft
  - [ ] Quick Draft (bot draft)
  - [ ] Traditional Draft

- [ ] **Track the draft in the model.** *Blocked on parsing.*
  - `app.Model` gains the current draft: set, format, pack and pick number,
    the pack's card ids, the picks so far. Pure transitions from the parsed
    events, through `Apply`.
  - The window shows the pack as card ids, with pack and pick number, and the
    picks so far. Names arrive with the next task; this is the honest
    intermediate view, not a placeholder.
  - Tests: a whole draft replayed from fixtures ends with the right pool; a
    reconnect mid-draft does not duplicate picks.

- [ ] **Card names and details from Scryfall, cached offline.**
  - The app's HTTP client (decision 9): User-Agent, per-host rate limiter,
    context, conditional requests. Goes in `fetch/`; every later source uses it.
  - The SQLite store (decision 11): schema with migrations, sets and cards
    keyed by Arena id. Raise the size budget in the same PR, with the measured
    cost.
  - When a draft starts in a set not in the store, fetch that set's cards from
    Scryfall's paged search and store them; never the bulk file. Subsequent
    drafts of the set make no Scryfall API requests.
  - The pack shows names, mana cost, type, and rarity. Cards Scryfall does not
    know yet show by id with a note (decision 15). A visible loading state while
    the set is fetched; a clear message if the network is down, and the ids
    still shown.
  - Update the network budget in `docs/ARCHITECTURE.md` if reality differs.
  - Tests: `httptest` with recorded pages; paging; a 429 backs off and
    retries; a cache hit makes no request; offline; an unwritable cache dir.

- [ ] **Ratings from 17lands, and the pack ranked by them.**
  - First confirm 17lands' current endpoints, response fields, and terms, and
    record them in decision 13. If the terms are unclear, stop and mark this
    task **needs human**.
  - Fetch ratings for the draft's set and format, store them, and refresh at
    most once a day.
  - The pack is sorted by rating, best first, with the number and what it is
    (for example, "GIH WR 58.2%"), and the sample size where it is small enough
    to matter. Cards with no rating sort last and say "no data yet".
  - 17lands is credited in the window.
  - Tests: ranking with ties and missing ratings; a stale rating is refreshed
    once; offline uses what is stored.

- [ ] **Card images, cached and capped.**
  - The image store (decision 12): downloads Scryfall `normal` images through the
    app's client, at most 4 at once, into the cache dir; 200 MB cap with LRU
    eviction; hands go-gui local paths only.
  - The pack shows card images in a grid that reflows with the window width,
    with each card's rating on it. A placeholder with the card's name shows
    until the image arrives.
  - Only cards on screen are fetched.
  - Tests: eviction order; the cap holds across restarts; a failed download
    retries later, not every frame; a corrupt file is replaced.

- [ ] **The picks-so-far panel.**
  - Colour counts, a mana curve, and creature/non-creature split of the picks,
    beside or under the pack depending on window width.
  - Pure summary functions, tested on their own.

- [ ] **Letter grades from a 17lands tier list.**
  - The user pastes a 17lands tier list link; it is validated, stored in a small
    settings file under `os.UserConfigDir()`, and fetched at most once a day.
  - Each card shows its grade beside its stats, labelled with the list's name.
  - Tests: a bad link says what a good one looks like; a list for a different set
    is ignored for this draft with a note.

- [ ] **Release builds for macOS and Linux.**
  - A tag-triggered workflow that builds a macOS `.app` (zipped, arm64 and
    amd64) and a Linux tarball (amd64 and arm64) with a `.desktop` file, and
    attaches them with SHA-256 checksums.
  - README install section updated.
  - Signing and notarisation for macOS are a separate **needs human** item:
    without them, Gatekeeper warns on first launch, and the README says how to
    open it anyway.

- [ ] **Needs human: macOS signing and notarisation.**
  Needs an Apple Developer account and its credentials as repository secrets.

### Milestone 2 — Building the deck

- [ ] **The pool, after the draft.** All picks grouped by colour and sorted by
  rating, with the curve, shown when the draft ends.
- [ ] **Suggested colours.** The colour pairs whose best 23 playables rate
  highest, with the reasoning shown (which cards carry it), not just an answer.
- [ ] **A 40-card build.** A suggested 23 spells and 17 lands for the chosen
  colours, editable, and copied to the clipboard in Arena's deck-import format.
- [ ] **Sealed.** The same pool and build views for a sealed pool.

### Ideas, not yet scheduled

Each needs a decision from the maintainer before it becomes a task.

- **Windows.** Build, test, and release on Windows after the MVP (decision 3).
- **An overlay on Arena's window** instead of, or as well as, the companion
  window (decision 18).
- **A light theme, and following the OS appearance** (decision 7).
- **Arena's local card database as a fallback** for cards Scryfall does not know
  yet (decision 15).
- **Signals:** which colours are flowing, from what is still in packs late.
- **Wheel prediction:** which cards in this pack are likely to come back.
- **Draft history:** past drafts and how they did, from the log.
- **More places to find the log.** Steam libraries on a second drive (read
  from Steam's `libraryfolders.vdf`) and the Heroic launcher's prefixes. Each
  means reading another application's files, whose layout should come from a
  real install, not from memory (decision 20).
- **A startup-time gate in CI.** The time to first frame is reported on every
  PR (decision 19) but not gated; whether to fail CI on it, and at what
  number, waits on enough CI runs to see how much it varies.

---

## Done

### Bootstrap

- [x] Project skeleton: window with first-paint empty state, headless view tests,
  lint rules, CI with smoke launch and size budget, nightly loop. (1–18)

### Milestone 1

- [x] Time to first frame: `-startup-time` reports it by stage, printed by
  `make smoke` and CI's macOS job; the accessibility bridge measured. (19)
- [x] Finding Arena's log: the usual places on macOS, Windows, and Linux prefixes
  (Wine, Lutris, Bottles, Steam), `-log`, a waiting state that says where it
  looked, and panics in background tasks shown in the window. (6, 20, 21)
