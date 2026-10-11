# MTGA Draft Assistant

[![CI](https://github.com/nmcginn/mtga-draft-assistant/actions/workflows/ci.yml/badge.svg)](https://github.com/nmcginn/mtga-draft-assistant/actions/workflows/ci.yml)

A small, quiet companion window for drafting in MTG Arena. It watches the log
Arena writes as you play, and shows the pack in front of you ranked by rating,
with card images, so the strong picks stand out at a glance. Later, it helps you
build the deck from what you drafted.

> **Status: early.** The window opens and waits for Arena; draft tracking is
> being built one pull request at a time. See [`docs/ROADMAP.md`](docs/ROADMAP.md).

## What it does, and what it doesn't

- **Reads, never writes.** It reads Arena's `Player.log`. It never touches
  Arena's files, memory, or network traffic, and it never clicks anything for you.
- **Light on the network.** Card data and images come from
  [Scryfall](https://scryfall.com), and ratings from [17lands](https://www.17lands.com).
  Each is fetched once and kept on disk. A set you have drafted before costs no
  requests beyond a once-a-day ratings check. No telemetry, no update checks, no
  account.
- **Light on your machine.** No CPU while nothing is happening. The image cache
  is capped (200 MB by default) and cleans up after itself.

## Setup

1. In Arena, open **Options → Account** and turn on
   **Detailed Logs (Plugin Support)**. Without it, Arena doesn't log drafts.
2. Start the assistant, then Arena (in either order).

Coming with the first roadmap tasks: the assistant will find Arena's log on
its own on macOS and inside common Wine/Proton prefixes on Linux, with a
`-log "/path/to/Player.log"` flag for anywhere else.

## Install

Release builds for macOS and Linux will be published on the
[releases page](https://github.com/nmcginn/mtga-draft-assistant/releases). Until
then, build from source.

### From source

Needs Go 1.26 or newer (the exact toolchain in `go.mod` is downloaded
automatically). On macOS, you also need the Xcode command-line tools
(`xcode-select --install`). On Linux, the window needs X11 (or experimental
Wayland via `GOGUI_WAYLAND=1`) and `libEGL`.

```sh
git clone https://github.com/nmcginn/mtga-draft-assistant
cd mtga-draft-assistant
make release
./bin/mtga-draft-assistant
```

## Where it keeps things

Everything it downloads lives in one cache directory, and deleting it is always
safe. It is fetched again as needed.

| Platform | Cache |
| -------- | ----- |
| macOS | `~/Library/Caches/mtga-draft-assistant` |
| Linux | `$XDG_CACHE_HOME/mtga-draft-assistant`, usually `~/.cache/mtga-draft-assistant` |

## Development

```sh
make help     # every target
make check    # what CI runs: format, vet, lint, race tests, cross-compile, size
make run      # build and launch
make smoke    # launch the release build, screenshot it, check it idles (Linux)
```

`-startup-time` prints how long the first frame took to reach the screen, and
where the time went; `scripts/first-frame.sh bin/mtga-draft-assistant 3`
launches it three times and prints each.

Work on this project happens in small nightly pull requests; the working
agreement is in [`CLAUDE.md`](CLAUDE.md), the design in
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), and the reasons behind it in
[`docs/DECISIONS.md`](docs/DECISIONS.md).

## Credits

Card data and images are provided by [Scryfall](https://scryfall.com). Ratings
are from [17lands](https://www.17lands.com). Built with
[go-gui](https://github.com/go-gui-org/go-gui).

MTGA Draft Assistant is unofficial Fan Content permitted under the Fan Content
Policy. Not approved or endorsed by Wizards. Portions of the materials used are
property of Wizards of the Coast. ©Wizards of the Coast LLC.

## License

[MIT](LICENSE)
