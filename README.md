# MTGA Draft Assistant

[![CI](https://github.com/nmcginn/mtga-draft-assistant/actions/workflows/ci.yml/badge.svg)](https://github.com/nmcginn/mtga-draft-assistant/actions/workflows/ci.yml)

A small, quiet companion window for drafting in MTG Arena. It watches the log
Arena writes as you play, and shows the pack in front of you ranked by rating,
with card images, so the strong picks stand out at a glance. Later, it helps you
build the deck from what you drafted.

> **Status: early.** It follows a Premier or Quick Draft and shows the pack
> with card names and pictures, and your picks counted up beside it. Ratings,
> and so the ranking, are not in yet. See [`docs/ROADMAP.md`](docs/ROADMAP.md).

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
2. Start the assistant, then Arena (in either order). It keeps reading the
   log as Arena writes it, and carries on by itself when Arena restarts.

The assistant finds Arena's log on its own on macOS, on Windows, and on Linux
inside Wine, Lutris, Bottles, and Steam (Proton) prefixes. While it waits, it
lists where it is looking. If Arena is installed somewhere else, point it at the
log:

```sh
mtga-draft-assistant -log "/path/to/Player.log"
```

## Install

Download the build for your machine from the
[releases page](https://github.com/nmcginn/mtga-draft-assistant/releases).
Each release lists the SHA-256 of every file in `SHA256SUMS`; compare it with
`shasum -a 256 <file>` (macOS) or `sha256sum <file>` (Linux).

### macOS

Needs macOS 12 Monterey or later. Download
`mtga-draft-assistant-<version>-macos-arm64.zip` for Apple silicon (M1 and
later) or `…-macos-amd64.zip` for an Intel Mac, open it, and drag
**MTGA Draft Assistant** into Applications.

The app is not yet signed with an Apple Developer ID, so the first time you
open it macOS says it cannot verify it and offers to move it to the Trash. Choose
**Done**, then open **System Settings → Privacy & Security**, scroll down to
**Security**, and click **Open Anyway** beside the assistant's name. After
that it opens like any other app. (On macOS 14 and earlier, Control-clicking
the app and choosing **Open** does the same.)

To give it flags such as `-log`, run it from Terminal:

```sh
"/Applications/MTGA Draft Assistant.app/Contents/MacOS/mtga-draft-assistant" -log "/path/to/Player.log"
```

### Linux

Needs X11 (or experimental Wayland via `GOGUI_WAYLAND=1`) and `libEGL`
(`libegl1` on Debian and Ubuntu). Download
`mtga-draft-assistant-<version>-linux-amd64.tar.gz` (or `…-arm64` on ARM), and
install the binary and its launcher for your user:

```sh
tar -xzf mtga-draft-assistant-<version>-linux-amd64.tar.gz
cd mtga-draft-assistant-<version>-linux-amd64
install -Dm755 mtga-draft-assistant ~/.local/bin/mtga-draft-assistant
install -Dm644 mtga-draft-assistant.desktop ~/.local/share/applications/mtga-draft-assistant.desktop
```

It then appears among your applications as **MTGA Draft Assistant**. The
launcher runs `mtga-draft-assistant` from your `PATH`, which includes
`~/.local/bin` on most distributions; if yours does not, install the binary
somewhere that is. To uninstall, delete those two files (and, if you like, the
cache below).

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
make dist VERSION=v1.2.3   # this OS's release packages, both architectures, into dist/
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
