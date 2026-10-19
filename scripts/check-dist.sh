#!/bin/sh
# Checks the packages scripts/dist.sh built, as a user would meet them:
# unpacks each one, checks it holds what it should, and runs every binary
# this machine can run with -version (decision 29).
#
#   scripts/check-dist.sh <version>
#
# On Linux it needs desktop-file-validate (desktop-file-utils); on macOS,
# only what Xcode's command-line tools bring.
set -eu

if [ "$#" -ne 1 ]; then
	echo "usage: $0 <version>" >&2
	exit 2
fi
version=$1
name=mtga-draft-assistant
want="$name $version"
host_arch=$(go env GOARCH)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

fail() {
	echo "$*" >&2
	exit 1
}

# runs <binary> [launcher...]: -version must name this release, or the
# version stamp went missing between the tag and the binary.
runs() {
	bin=$1
	shift
	got=$("$@" "$bin" -version) || fail "$bin -version failed"
	[ "$got" = "$want" ] || fail "$bin -version says '$got', want '$want'"
	echo "  runs: $got"
}

case $(uname -s) in
Linux)
	command -v desktop-file-validate >/dev/null ||
		fail "desktop-file-validate is missing; install desktop-file-utils"
	for arch in amd64 arm64; do
		pkg=$name-$version-linux-$arch
		tarball=dist/$pkg.tar.gz
		[ -f "$tarball" ] || fail "no $tarball; build it with 'make dist VERSION=$version'"
		echo "$tarball"

		listing=$(tar -tzf "$tarball" | sort)
		expected=$(printf '%s\n' "$pkg/" "$pkg/LICENSE" "$pkg/README.md" "$pkg/$name" "$pkg/$name.desktop" | sort)
		[ "$listing" = "$expected" ] || fail "$tarball holds:
$listing
want:
$expected"

		tar -C "$work" -xzf "$tarball"
		bin=$work/$pkg/$name
		[ -x "$bin" ] || fail "$bin is not executable"
		desktop-file-validate "$work/$pkg/$name.desktop"
		echo "  $name.desktop is valid"

		# The ELF header's machine field: 62 is x86-64, 183 is AArch64.
		machine=$(od -An -tu2 -j18 -N2 "$bin" | tr -d ' ')
		case $arch in
		amd64) [ "$machine" = 62 ] || fail "$bin is not an x86-64 binary (ELF machine $machine)" ;;
		arm64) [ "$machine" = 183 ] || fail "$bin is not an AArch64 binary (ELF machine $machine)" ;;
		esac
		echo "  built for $arch"
		if [ "$arch" = "$host_arch" ]; then
			runs "$bin"
		else
			echo "  not run: this machine is $host_arch"
		fi
	done
	;;
Darwin)
	short=${version#v}
	short=${short%%-*}
	for arch in arm64 amd64; do
		zip=dist/$name-$version-macos-$arch.zip
		[ -f "$zip" ] || fail "no $zip; build it with 'make dist VERSION=$version'"
		echo "$zip"

		mkdir -p "$work/$arch"
		ditto -x -k "$zip" "$work/$arch"
		app="$work/$arch/MTGA Draft Assistant.app"
		[ -d "$app" ] || fail "$zip does not hold 'MTGA Draft Assistant.app'"
		bin="$app/Contents/MacOS/$name"
		plist="$app/Contents/Info.plist"

		plutil -lint -s "$plist"
		got=$(plutil -extract CFBundleShortVersionString raw "$plist")
		[ "$got" = "$short" ] || fail "Info.plist says version '$got', want '$short'"
		got=$(plutil -extract CFBundleExecutable raw "$plist")
		[ "$got" = "$name" ] || fail "Info.plist names '$got' to run, want '$name'"
		echo "  Info.plist: version $short"

		codesign --verify --deep --strict "$app" || fail "the app's signature does not verify"
		echo "  signature verifies (ad hoc)"

		got=$(lipo -archs "$bin")
		case $arch in
		arm64) [ "$got" = arm64 ] || fail "$bin is built for '$got', want arm64" ;;
		amd64) [ "$got" = x86_64 ] || fail "$bin is built for '$got', want x86_64" ;;
		esac
		echo "  built for $got"

		# The binary must run on the macOS Info.plist promises, not only on
		# the runner that built it (scripts/dist.sh).
		min=$(plutil -extract LSMinimumSystemVersion raw "$plist")
		minos=$(otool -l "$bin" | awk '$1 == "minos" { print $2; exit }')
		[ "$minos" = "$min" ] || fail "$bin needs macOS $minos, but Info.plist promises $min"
		echo "  needs macOS $minos or later"

		if [ "$arch" = "$host_arch" ]; then
			runs "$bin"
		elif [ "$arch" = amd64 ] && arch -x86_64 /usr/bin/true 2>/dev/null; then
			runs "$bin" arch -x86_64
		else
			echo "  not run: this machine is $host_arch, without Rosetta"
		fi
	done
	;;
*)
	fail "release packages are checked on Linux or macOS, not $(uname -s)"
	;;
esac
echo "Packages check out."
