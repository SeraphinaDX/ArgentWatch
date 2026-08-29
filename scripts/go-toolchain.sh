#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-or-later
#
# ArgentWatch build toolchain selector.
#
# Selection order:
#   1. An external `zgo` wrapper, if present.
#   2. An installed `zig`, using ArgentWatch's bundled zgo-style wrapper.
#   3. Ordinary Go with CGO enabled and the system C/C++ compiler.
#
# Zig itself installs `zig`; it does not normally install a `zgo` executable.
# The bundled wrapper makes Go's CGO compiler invocations go through `zig cc`
# and `zig c++` when Zig is present.

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ZGO_BIN=${ZGO:-zgo}
ZIG_BIN=${ZIG:-zig}
GO_BIN=${GO:-go}

have_cmd() {
    command -v "$1" >/dev/null 2>&1
}

print_selection() {
    if have_cmd "$ZGO_BIN"; then
        resolved=$(command -v "$ZGO_BIN")
        printf '%s\n' "external zgo ($resolved)"
    elif have_cmd "$ZIG_BIN"; then
        resolved=$(command -v "$ZIG_BIN")
        printf '%s\n' "bundled zgo wrapper -> $resolved cc/c++"
    else
        printf '%s\n' "ordinary Go/CGO fallback ($GO_BIN; system C/C++ compiler)"
    fi
}

if [ "${1:-}" = "--print" ]; then
    print_selection
    exit 0
fi

if have_cmd "$ZGO_BIN"; then
    printf '%s\n' "ArgentWatch: using external zgo ($ZGO_BIN)" >&2
    exec "$ZGO_BIN" "$@"
fi

if have_cmd "$ZIG_BIN"; then
    printf '%s\n' "ArgentWatch: zgo not found; using Zig through bundled zgo wrapper ($ZIG_BIN cc/c++)" >&2
    exec env ZIG="$ZIG_BIN" GO="$GO_BIN" "$SCRIPT_DIR/zgo" "$@"
fi

printf '%s\n' "ArgentWatch: zgo and zig not found; falling back to Go with ordinary CGO" >&2
exec env CGO_ENABLED=1 "$GO_BIN" "$@"
