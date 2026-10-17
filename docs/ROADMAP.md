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
  - [ ] A Premier Draft resumed after relaunching Arena mid-draft, from the
    relaunch through the next pick. No fixture shows what a draft against
    people logs on rejoining, so the parser cannot yet tell such a draft's
    event or set (decision 23).
  - [ ] Scryfall's search for FRA, recorded, to replace the pages under
    `testdata/scryfall/`, which were written to Scryfall's documented schema
    because the session that built the client could not reach it. The
    command and what to keep are in `testdata/scryfall/README.md`. The tests
    name the cards they expect, so those names change with the pages. Card
    images (decision 27) were built on the same written pages, so a
    recording also confirms where Scryfall puts a card's picture.

- [ ] **Parse the remaining draft formats.** *Each blocked on its fixture.*
  Whichever of these the parsing task did not already cover, one PR per
  format as its fixture lands: the parser learns that format's lines, and the
  same tests run against its fixture, including a whole draft replayed through
  the model once the next task exists. Tick a format here when it is
  covered, by either task.
  - [x] Premier Draft
  - [x] Quick Draft (bot draft)
  - [ ] Traditional Draft

- [ ] **Ratings from 17lands, and the pack ranked by them.** *Blocked on
  the next item.*
  - Fetch ratings for the draft's set and format, store them, and refresh at
    most once a day.
  - The pack is sorted by rating, best first, with the number and what it is
    (for example, "GIH WR 58.2%"), and the sample size where it is small enough
    to matter. Cards with no rating sort last and say "no data yet". The
    rating is on each card in the pack's grid: on its picture, and on its
    placeholder while the picture is coming.
  - 17lands is credited in the window.
  - Tests: ranking with ties and missing ratings; a stale rating is refreshed
    once; offline uses what is stored.

- [ ] **Needs human: confirm 17lands' endpoints and terms of use.** Decision
  13 asks for them before the first fetch ships. The nightly session of
  2026-10-17 could not reach `17lands.com` (its network policy refuses the
  host) and found no published terms by web search, so it could confirm
  neither. Needed: the endpoint and fields that give a set and format's
  card ratings (a recorded response for `testdata/`, as with Scryfall), and
  whether 17lands allows a desktop tool to fetch them once a day per set,
  with credit. Record both in decision 13.

- [ ] **The picks-so-far panel.**
  - Colour counts, a mana curve, and creature/non-creature split of the picks,
    beside or under the pack depending on window width.
  - Pure summary functions, tested on their own.

- [ ] **Letter grades from a 17lands tier list.** *Blocked on 17lands'
  terms, as above.*
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
- **Cards from outside the drafted set.** A pack holding a card printed in
  another set (a bonus sheet) would show it as "Not on Scryfall yet", since
  only the drafted set is searched. Neither fixture's packs hold one; a
  fixture that does would show which set to search, or whether to look such
  cards up one by one (decision 26).
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
- [x] Following the log: only new bytes, partial lines held back, restart and
  loss detected, a loading state while catching up, read errors with what to
  do, and the smoke job measuring idle while following. (14, 22)
- [x] Parsing draft events: Premier and Quick Draft from their fixtures, cut
  and repeated lines, and formats with no fixture skipped. (23)
- [x] Tracking the draft: the log's draft events reach the model, and the
  window shows the pack by card id with pack and pick, the picks so far, and
  the pool when it ends; repeated picks and a resumed bot draft read once. (24)
- [x] Card names from Scryfall, cached offline: the app's HTTP client, the
  SQLite store, a set fetched once by paged search and refreshed weekly, the
  pack, picks and pool by name with cost, type and rarity, and loading,
  offline and unusable-cache states that say what to do. (11, 15, 25, 26)
- [x] Card images, cached and capped: Scryfall's `normal` picture for each
  card in the pack, at most 4 at once, kept under a 200 MB cap with
  least-recently-used eviction that holds across restarts; a grid that
  reflows with the window, a placeholder with the card's name meanwhile,
  failures retried on their own schedule, and corrupt files replaced. (12, 27)
