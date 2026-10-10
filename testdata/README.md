# Test fixtures

Log parsing is only as right as the logs it was tested against, and Arena's log
format is undocumented and changes with game updates. So every parser test
reads a fixture cut from a **real** `Player.log`, never one written from memory
or from another tool's source code.

## Layout

```
testdata/
  logs/
    <yyyy-mm>-<set>-<format>-<what>.log   e.g. 2026-10-dsk-premier-pack1pick1.log
  scryfall/                               recorded API responses, trimmed
  17lands/                                recorded API responses, trimmed
```

The date is when the log was captured, so when Arena changes its format the
fixtures from before and after can both be kept and both be tested.

## Capturing a log fixture

1. In Arena, turn on **Options → Account → Detailed Logs (Plugin Support)**.
2. Play the draft you want to capture (a few picks is often enough).
3. Copy the relevant lines out of `Player.log` — the draft's requests and
   responses, plus a few surrounding lines so the parser sees realistic noise.
   Keep whole lines: a fixture with a line cut off in the middle is its own,
   deliberate test case, and is named that way.
4. **Scrub it** before committing. Logs contain account details. Replace, with
   a stable fake value for each one:
   - your display name and its `#12345` suffix,
   - account and player IDs (`userId`, `playerId`, `personaId`, and any GUIDs
     that identify you rather than an event or a card),
   - session and authentication tokens (anything named `token`, `ticket`, or
     `session`),
   - email addresses and IP addresses,
   - IDs of things the account owns: course (`CourseId`) and deck (`DeckId`)
     IDs,
   - paths under your home directory, and Steam IDs.
   Card IDs (`grpId`), event names, set codes, and draft IDs are not personal
   and should stay as they are.

   The repository is public, so be conservative beyond that list: anything a
   parser does not need is zeroed, not kept because it looks harmless. Keep
   every key, so the line keeps its real shape, and zero the values: numbers
   to `0`, strings to `""`, lists to `[]`, and maps keyed by what the account
   owns (`CustomTokens`, `Vouchers`) to `{}`. In practice that means the whole
   `InventoryInfo` object (currency, wildcards, cosmetics owned), the
   cosmetics on a deck (`Sleeve`, `DeckArtId`), and any timestamp that
   carries a UTC offset. Log line timestamps (`10/10/2026 12:44:31 PM`) stay:
   they are part of the log's format.
5. Commit it with a note in the PR saying which Arena version it came from.

## Recorded HTTP responses

Trim responses to the cards a test needs, keep the original structure and field
names, and note the date captured at the top of the test that uses them. Tests
serve them from `httptest.Server`; nothing in the test suite touches the real
services.
