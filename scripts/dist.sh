#!/bin/sh
# Builds the release packages for the OS it runs on into dist/, one per
# architecture (decision 29):
#
#   Linux: mtga-draft-assistant-<version>-linux-<arch>.tar.gz, a folder holding
#          the binary, a .desktop file, the README and the licence.
#   macOS: mtga-draft-assistant-<version>-macos-<arch>.zip, holding
#          "MTGA Draft Assistant.app", signed ad hoc (not notarised).
#
#   scripts/dist.sh <version> <budget-in-MiB>
#
# <version> is a tag such as v1.2.3 or v1.2.3-rc.1; it is what -version and
# the User-Agent say. Each binary is held to the size budget, as `make size`
# holds the host's. macOS is built on macOS, both architectures from one
# machine: cgo cross-compiles between them, where it cannot from Linux
# (decision 3). The release workflow runs this on each OS, and CI runs it on
# every PR with scripts/check-dist.sh, so a tag is never the first time the
# packaging runs.
set -eu

if [ "$#" -ne 2 ]; then
	echo "usage: $0 <version> <budget-in-MiB>" >&2
	exit 2
fi
version=$1
budget=$2

if ! printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; then
	echo "'$version' is not a release version; give one like v1.2.3 or v1.2.3-rc.1:" >&2
	echo "  make dist VERSION=v1.2.3" >&2
	exit 2
fi

# Go's own floor for macOS: its linker writes 12.0 into every binary it
# links itself. A cgo binary is linked by clang instead, which otherwise
# writes the building machine's macOS, so an app built on the newest runner
# would refuse to start on anything older.
min_macos=12.0

name=mtga-draft-assistant
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# dist/ holds one release at a time, so what is uploaded from it is only this
# version's packages.
mkdir -p dist
rm -f dist/$name-* dist/SHA256SUMS

# build <goos> <goarch> <cgo> <out>: the same flags as `make release`, which
# owns them.
build() {
	GOOS=$1 GOARCH=$2 CGO_ENABLED=$3 make --no-print-directory -s release VERSION="$version" OUT="$4"
	scripts/check-size.sh "$4" "$budget"
}

case $(uname -s) in
Linux)
	for arch in amd64 arm64; do
		pkg=$name-$version-linux-$arch
		mkdir -p "$work/$pkg"
		build linux "$arch" 0 "$work/$pkg/$name"
		cp packaging/linux/$name.desktop README.md LICENSE "$work/$pkg/"
		# Owned by root in the archive, not by whichever runner built it.
		tar -C "$work" --owner=0 --group=0 --numeric-owner -czf "dist/$pkg.tar.gz" "$pkg"
		echo "dist/$pkg.tar.gz"
	done
	;;
Darwin)
	# x.y.z only: Finder and the App Store tools reject a pre-release suffix.
	short=${version#v}
	short=${short%%-*}
	for arch in arm64 amd64; do
		app="$work/$arch/MTGA Draft Assistant.app"
		mkdir -p "$app/Contents/MacOS"
		MACOSX_DEPLOYMENT_TARGET=$min_macos build darwin "$arch" 1 "$app/Contents/MacOS/$name"
		sed -e "s/@SHORT_VERSION@/$short/g" -e "s/@MIN_MACOS@/$min_macos/g" \
			packaging/macos/Info.plist >"$app/Contents/Info.plist"
		plutil -lint -s "$app/Contents/Info.plist"
		# Ad hoc, so the signature seals the bundle and its Info.plist; Apple
		# silicon will not run an unsigned binary at all. Without a Developer
		# ID this is still unidentified to Gatekeeper, and the README says how
		# to open it the first time.
		codesign --force --sign - "$app"
		# ditto, not zip: it keeps the bundle's structure and signature whole.
		ditto -c -k --keepParent "$app" "dist/$name-$version-macos-$arch.zip"
		echo "dist/$name-$version-macos-$arch.zip"
	done
	;;
*)
	echo "release packages are built on Linux or macOS, not $(uname -s)" >&2
	exit 2
	;;
esac
