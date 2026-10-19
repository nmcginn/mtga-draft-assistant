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

# The oldest macOS the app runs on. Go's own floor is 12, but go-gui's Metal
# backend asks for Metal's 3.0 shading language, which macOS 13 brought. A
# cgo binary is linked by clang, which otherwise writes the building
# machine's macOS into it, so an app built on the newest runner would refuse
# to start on anything older.
min_macos=13.0

name=mtga-draft-assistant
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# dist/ holds one release at a time, so what is uploaded from it is only this
# version's packages.
mkdir -p dist
rm -f dist/$name-* dist/SHA256SUMS

# build <goos> <goarch> <cgo> <out>: the same flags as `make release`, which
# owns them. The compiler's warnings are kept and read, because two of them
# mean the binary would not run on the macOS it promises: code that needs a
# newer macOS than min_macos, and objects compiled for one.
build() {
	if ! GOOS=$1 GOARCH=$2 CGO_ENABLED=$3 make --no-print-directory -s release \
		VERSION="$version" OUT="$4" 2>"$work/stderr"; then
		cat "$work/stderr" >&2
		exit 1
	fi
	cat "$work/stderr" >&2
	if grep -Eq "is only available on macOS|built for newer 'macOS' version" "$work/stderr"; then
		echo "Part of the app needs a newer macOS than the $min_macos it promises (above)." >&2
		echo "'is only available': a dependency now needs that macOS; raise min_macos in $0," >&2
		echo "and the README's macOS line with it. 'built for newer': the -mmacosx-version-min" >&2
		echo "flags did not reach that object; check the CGO_CFLAGS this script sets." >&2
		exit 1
	fi
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
		# The target in the flags as well as the environment: Go's build cache
		# keys on CGO_CFLAGS but not on MACOSX_DEPLOYMENT_TARGET, so without
		# them cgo objects cached by a plain build, compiled for the runner's
		# macOS, are linked in as they are. -O2 -g are Go's defaults.
		MACOSX_DEPLOYMENT_TARGET=$min_macos \
			CGO_CFLAGS="-O2 -g -mmacosx-version-min=$min_macos" \
			CGO_LDFLAGS="-O2 -g -mmacosx-version-min=$min_macos" \
			build darwin "$arch" 1 "$app/Contents/MacOS/$name"
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
