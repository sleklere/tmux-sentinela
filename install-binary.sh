#!/usr/bin/env bash
set -u

dest=${1:?destination path required}
base=${TMUX_SENTINELA_RELEASE_BASE:-https://github.com/sleklere/tmux-sentinela/releases/latest/download}

case "${TMUX_SENTINELA_OS:-$(uname -s)}" in
	Linux | linux) os=linux ;;
	Darwin | darwin) os=darwin ;;
	*) printf 'unsupported operating system\n' >&2; exit 1 ;;
esac

case "${TMUX_SENTINELA_ARCH:-$(uname -m)}" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) printf 'unsupported architecture\n' >&2; exit 1 ;;
esac

fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1" -o "$2"
	elif command -v wget >/dev/null 2>&1; then
		wget -q "$1" -O "$2"
	else
		return 1
	fi
}

asset="tmux-sentinela-$os-$arch"
mkdir -p "$(dirname "$dest")"
binary="$dest.download.$$"
checksums="$dest.checksums.$$"
trap 'rm -f "$binary" "$checksums"' EXIT

if ! fetch "$base/$asset" "$binary" || ! fetch "$base/checksums.txt" "$checksums"; then
	printf 'could not download %s (curl or wget required)\n' "$asset" >&2
	exit 1
fi

expected=
while read -r sum name; do
	if [ "$name" = "$asset" ]; then
		expected=$sum
		break
	fi
done < "$checksums"

if command -v sha256sum >/dev/null 2>&1; then
	read -r actual _ < <(sha256sum "$binary")
elif command -v shasum >/dev/null 2>&1; then
	read -r actual _ < <(shasum -a 256 "$binary")
else
	printf 'sha256sum or shasum is required\n' >&2
	exit 1
fi

if [ -z "$expected" ] || [ "$actual" != "$expected" ]; then
	printf 'checksum mismatch for %s\n' "$asset" >&2
	exit 1
fi

chmod 0755 "$binary"
mv -f "$binary" "$dest"
