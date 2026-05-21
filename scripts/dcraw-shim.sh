#!/bin/sh
#
# dcraw-shim.sh — minimal dcraw replacement implemented on top of
# libraw-tools' `dcraw_emu`. Alpine dropped the upstream `dcraw` package
# (Dave Coffin stopped maintaining it; LibRaw is the modern replacement),
# but the Go upload pipeline still shells out to `dcraw -c -w -h <file>`
# and expects a binary PPM frame on stdout. This shim accepts that exact
# command line and emits the matching PPM stream so the pipeline keeps
# working without any Go changes.
#
# Supported flags (the only ones the Go code emits):
#   -c           write to stdout
#   -w           use camera white balance
#   -h           half-size decode
#
# Unsupported flags cause the shim to exit non-zero so the failure is loud
# rather than silently producing a wrong-format file.

set -eu

write_stdout=0
emu_args=""
input=""

while [ $# -gt 0 ]; do
    case "$1" in
        -c) write_stdout=1 ;;
        -w) emu_args="$emu_args -w" ;;
        -h) emu_args="$emu_args -h" ;;
        --) shift; input="${1:-}"; break ;;
        -*)
            echo "dcraw-shim: unsupported flag $1" >&2
            exit 64
            ;;
        *) input="$1" ;;
    esac
    shift
done

if [ -z "$input" ]; then
    echo "dcraw-shim: no input file" >&2
    exit 64
fi
if [ ! -r "$input" ]; then
    echo "dcraw-shim: cannot read $input" >&2
    exit 66
fi

# dcraw_emu writes <input>.ppm next to the input file. The Go pipeline
# hands us read-only originals from a managed directory, so stage a
# symlink in a fresh temp dir and let dcraw_emu write the PPM there.
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

# Use a basename without extension so the output is predictable
# ("<tmpdir>/raw.ppm"). The symlink avoids copying potentially huge RAWs.
ln -s "$(readlink -f "$input")" "$tmpdir/raw"

# shellcheck disable=SC2086  # emu_args is an intentional split
dcraw_emu $emu_args "$tmpdir/raw" >/dev/null

ppm="$tmpdir/raw.ppm"
if [ ! -s "$ppm" ]; then
    echo "dcraw-shim: dcraw_emu produced no output for $input" >&2
    exit 70
fi

if [ "$write_stdout" -eq 1 ]; then
    cat "$ppm"
else
    # Without -c the original dcraw writes a .ppm next to the input. The
    # Go pipeline always passes -c so this branch is a safety net.
    cp "$ppm" "${input%.*}.ppm"
fi
