#!/usr/bin/env bash
#
# dump_account.sh — dump a single EVM account from BOTH FlatKV and memiavl
# at a given height and diff them.
#
# Typical usage on a live shadow node:
#
#   # 1. you suspect balance drift for address 0xABCD...:
#   scripts/flatkv/dump_account.sh \
#       --home /root/.sei \
#       --address 0xABCDEF0123456789ABCDEF0123456789ABCDEF01 \
#       --height 12345678
#
#   # exits 0 if the two backends agree byte-for-byte on that account,
#   # non-zero and prints a unified diff if they disagree.
#
# What it does, step by step:
#   - runs `seidb flatkv-account` against $HOME/data/flatkv
#   - runs `seidb iavl-account`   against $HOME/data/committer.db/memiavl.db
#     (or whatever layout the node uses — overridable via --flatkv-dir /
#     --memiavl-dir)
#   - writes both JSON dumps to a temp dir and runs `diff -u` between them
#
# Requires: seidb built at ./build/seidb (from `make install-seidb`) or on PATH.

set -euo pipefail

SEIDB_BIN="${SEIDB_BIN:-$(command -v seidb || true)}"
if [[ -z "$SEIDB_BIN" || ! -x "$SEIDB_BIN" ]]; then
    # Fall back to a local build in this repo.
    CANDIDATE="$(cd "$(dirname "$0")/../.." && pwd)/build/seidb"
    if [[ -x "$CANDIDATE" ]]; then
        SEIDB_BIN="$CANDIDATE"
    else
        echo "ERROR: seidb binary not found. Build it first:" >&2
        echo "  go build -o ./build/seidb ./sei-db/tools/cmd/seidb/" >&2
        echo "Or set SEIDB_BIN=/path/to/seidb." >&2
        exit 2
    fi
fi

usage() {
    cat <<EOF
Usage: $0 --address 0xHEX [--height N] (--home <seid-home> | --flatkv-dir D --memiavl-dir D)

Options:
  --address, -a 0xHEX        EVM account address (required)
  --height N                 block height (default: 0 == latest)
  --home PATH                shortcut: sets --flatkv-dir and --memiavl-dir from
                             PATH/data/{flatkv, committer.db/memiavl.db}
  --flatkv-dir PATH          override FlatKV data directory
  --memiavl-dir PATH         override memIAVL data directory
  --out-dir PATH             where to write the two JSON dumps
                             (default: a tempdir that is kept on diff mismatch)
  --quiet                    do not print dumps on success, only diff on mismatch
  -h, --help                 show this help
EOF
}

ADDRESS=""
HEIGHT=0
HOME_DIR=""
FLATKV_DIR=""
MEMIAVL_DIR=""
OUT_DIR=""
QUIET=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        -a|--address) ADDRESS="$2"; shift 2;;
        --height) HEIGHT="$2"; shift 2;;
        --home) HOME_DIR="$2"; shift 2;;
        --flatkv-dir) FLATKV_DIR="$2"; shift 2;;
        --memiavl-dir) MEMIAVL_DIR="$2"; shift 2;;
        --out-dir) OUT_DIR="$2"; shift 2;;
        --quiet) QUIET=1; shift;;
        -h|--help) usage; exit 0;;
        *) echo "unknown arg: $1" >&2; usage; exit 2;;
    esac
done

if [[ -z "$ADDRESS" ]]; then
    echo "ERROR: --address is required" >&2
    usage
    exit 2
fi

if [[ -n "$HOME_DIR" ]]; then
    : "${FLATKV_DIR:=$HOME_DIR/data/flatkv}"
    : "${MEMIAVL_DIR:=$HOME_DIR/data/committer.db/memiavl.db}"
fi

if [[ -z "$FLATKV_DIR" || -z "$MEMIAVL_DIR" ]]; then
    echo "ERROR: provide --home OR both --flatkv-dir and --memiavl-dir" >&2
    usage
    exit 2
fi

if [[ ! -d "$FLATKV_DIR" ]]; then
    echo "ERROR: FlatKV dir does not exist: $FLATKV_DIR" >&2
    exit 2
fi
if [[ ! -d "$MEMIAVL_DIR" ]]; then
    echo "ERROR: memIAVL dir does not exist: $MEMIAVL_DIR" >&2
    exit 2
fi

CLEANUP_ON_MATCH=1
if [[ -z "$OUT_DIR" ]]; then
    OUT_DIR="$(mktemp -d -t seidb-account.XXXXXX)"
else
    CLEANUP_ON_MATCH=0
    mkdir -p "$OUT_DIR"
fi

FLATKV_JSON="$OUT_DIR/flatkv.json"
MEMIAVL_JSON="$OUT_DIR/memiavl.json"

echo "> dumping $ADDRESS at height=$HEIGHT"
echo "  flatkv: $FLATKV_DIR"
echo "  memiavl: $MEMIAVL_DIR"
echo "  out: $OUT_DIR"

"$SEIDB_BIN" flatkv-account \
    --db-dir "$FLATKV_DIR" \
    --address "$ADDRESS" \
    --height "$HEIGHT" \
    --output "$FLATKV_JSON"

"$SEIDB_BIN" iavl-account \
    --db-dir "$MEMIAVL_DIR" \
    --address "$ADDRESS" \
    --height "$HEIGHT" \
    --output "$MEMIAVL_JSON"

# Normalize for diff: drop the `backend` and `rootHash` fields (they are
# expected to differ — FlatKV uses LtHash, memiavl uses IAVL hash — and
# the `backend` string is intentionally different). Everything else MUST
# be byte-identical if the two backends agree about this account.
norm() {
    # Strip the two expected-different fields; everything else is what we
    # are actually comparing. Falls back to the raw file if `jq` is not
    # installed, so the script still runs on a minimal node.
    if command -v jq >/dev/null 2>&1; then
        jq 'del(.backend, .rootHash)' "$1"
    else
        # jq-less fallback: just remove those two lines.
        grep -vE '^[[:space:]]*"(backend|rootHash)":' "$1"
    fi
}

NORM_FLATKV="$OUT_DIR/flatkv.norm.json"
NORM_MEMIAVL="$OUT_DIR/memiavl.norm.json"
norm "$FLATKV_JSON" > "$NORM_FLATKV"
norm "$MEMIAVL_JSON" > "$NORM_MEMIAVL"

if diff -q "$NORM_FLATKV" "$NORM_MEMIAVL" >/dev/null; then
    echo "OK: FlatKV and memIAVL agree on $ADDRESS at height=$HEIGHT"
    if [[ $QUIET -eq 0 ]]; then
        echo "--- dump ---"
        cat "$FLATKV_JSON"
    fi
    if [[ $CLEANUP_ON_MATCH -eq 1 ]]; then
        rm -rf "$OUT_DIR"
    fi
    exit 0
fi

echo "MISMATCH: FlatKV and memIAVL disagree on $ADDRESS at height=$HEIGHT"
echo "--- diff (flatkv -> memiavl) ---"
diff -u "$NORM_FLATKV" "$NORM_MEMIAVL" || true
echo "--- raw dumps retained at $OUT_DIR ---"
exit 1
