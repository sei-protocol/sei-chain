#!/usr/bin/env bash
#
# dump_account.sh — dump a single EVM account from BOTH FlatKV and memiavl
# at a given height and compare them.
#
# Typical usage on a live shadow node:
#
#   scripts/flatkv/dump_account.sh \
#       --home /root/.sei \
#       --address 0xABCDEF... \
#       --height 12345678
#
# Behaviour:
#   - exits 0 iff nonce, codeHash, codeSize and storageCount all match;
#     non-zero otherwise.
#   - always prints a side-by-side header summary (nonce / code / storage
#     counts) BEFORE touching the raw dumps, so the operator immediately
#     sees what's different without waiting on a multi-GB `diff`.
#   - only runs a full JSON diff when both storage sets are small enough
#     for diff to be useful (see MAX_SMALL_DIFF_BYTES). Beyond that, it
#     computes a jq-based slot-set intersection / missing-only / extra-only
#     report — much more useful than a unified diff on 8 GB of JSON.
#
# Requires: seidb on PATH (or SEIDB_BIN=<path>), jq for the rich diff.

set -euo pipefail

SEIDB_BIN="${SEIDB_BIN:-$(command -v seidb || true)}"
if [[ -z "$SEIDB_BIN" || ! -x "$SEIDB_BIN" ]]; then
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
  --home PATH                shortcut: sets --flatkv-dir and --memiavl-dir
  --flatkv-dir PATH          override FlatKV data directory
  --memiavl-dir PATH         override memIAVL data directory
  --out-dir PATH             where to keep the two JSON dumps
                             (default: tempdir, kept on mismatch only)
  --max-diff-bytes N         skip the full line-diff if either dump exceeds
                             this size in bytes (default: 50 MB)
  --quiet                    suppress the header dump on OK match
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
MAX_SMALL_DIFF_BYTES=${MAX_SMALL_DIFF_BYTES:-$((50 * 1024 * 1024))}

while [[ $# -gt 0 ]]; do
    case "$1" in
        -a|--address) ADDRESS="$2"; shift 2;;
        --height) HEIGHT="$2"; shift 2;;
        --home) HOME_DIR="$2"; shift 2;;
        --flatkv-dir) FLATKV_DIR="$2"; shift 2;;
        --memiavl-dir) MEMIAVL_DIR="$2"; shift 2;;
        --out-dir) OUT_DIR="$2"; shift 2;;
        --max-diff-bytes) MAX_SMALL_DIFF_BYTES="$2"; shift 2;;
        --quiet) QUIET=1; shift;;
        -h|--help) usage; exit 0;;
        *) echo "unknown arg: $1" >&2; usage; exit 2;;
    esac
done

[[ -z "$ADDRESS" ]] && { echo "ERROR: --address is required" >&2; usage; exit 2; }

if [[ -n "$HOME_DIR" ]]; then
    : "${FLATKV_DIR:=$HOME_DIR/data/flatkv}"
    : "${MEMIAVL_DIR:=$HOME_DIR/data/committer.db}"
fi

[[ -z "$FLATKV_DIR" || -z "$MEMIAVL_DIR" ]] && {
    echo "ERROR: provide --home OR both --flatkv-dir and --memiavl-dir" >&2
    exit 2
}
[[ ! -d "$FLATKV_DIR" ]]  && { echo "ERROR: FlatKV dir missing: $FLATKV_DIR" >&2; exit 2; }
[[ ! -d "$MEMIAVL_DIR" ]] && { echo "ERROR: memIAVL dir missing: $MEMIAVL_DIR" >&2; exit 2; }

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
echo "  flatkv:  $FLATKV_DIR"
echo "  memiavl: $MEMIAVL_DIR"
echo "  out:     $OUT_DIR"

t0=$(date +%s)
"$SEIDB_BIN" flatkv-account \
    --db-dir "$FLATKV_DIR" \
    --address "$ADDRESS" \
    --height "$HEIGHT" \
    --output "$FLATKV_JSON"
echo "  [flatkv-account] $(( $(date +%s) - t0 ))s, $(stat -c%s "$FLATKV_JSON" 2>/dev/null || stat -f%z "$FLATKV_JSON") bytes"

t0=$(date +%s)
"$SEIDB_BIN" iavl-account \
    --db-dir "$MEMIAVL_DIR" \
    --address "$ADDRESS" \
    --height "$HEIGHT" \
    --output "$MEMIAVL_JSON"
echo "  [iavl-account]   $(( $(date +%s) - t0 ))s, $(stat -c%s "$MEMIAVL_JSON" 2>/dev/null || stat -f%z "$MEMIAVL_JSON") bytes"

# ---- header summary (cheap; always runs) ----
# Intentionally grep-based even when jq is available: on a 8 GB dump jq
# builds a whole-file AST and blows up the machine. All header fields we
# care about (height, nonce, codeHash, codeSize, storageCount, address,
# rootHash) live in the first ~300 bytes or the last ~100 bytes, so a
# bounded grep is both correct and orders of magnitude cheaper.
extract() {
    # $1=file, $2=field
    grep -oE "\"${2}\"[[:space:]]*:[[:space:]]*[^,}]+" "$1" \
        | head -1 \
        | sed -E 's/.*:[[:space:]]*//; s/^"//; s/"$//'
}

F_H=$(extract "$FLATKV_JSON" height)
M_H=$(extract "$MEMIAVL_JSON" height)
F_NONCE=$(extract "$FLATKV_JSON" nonce)
M_NONCE=$(extract "$MEMIAVL_JSON" nonce)
F_CH=$(extract "$FLATKV_JSON" codeHash)
M_CH=$(extract "$MEMIAVL_JSON" codeHash)
F_CS=$(extract "$FLATKV_JSON" codeSize)
M_CS=$(extract "$MEMIAVL_JSON" codeSize)
F_SC=$(extract "$FLATKV_JSON" storageCount)
M_SC=$(extract "$MEMIAVL_JSON" storageCount)

printf '\n=== %s @ flatkv=%s / memiavl=%s ===\n' "$ADDRESS" "$F_H" "$M_H"
printf '  %-14s | %-42s | %-42s\n' field flatkv memiavl
printf '  %-14s | %-42s | %-42s\n' -------------- ------------------------------------------ ------------------------------------------
printf '  %-14s | %-42s | %-42s\n' nonce        "$F_NONCE"  "$M_NONCE"
printf '  %-14s | %-42s | %-42s\n' codeSize     "$F_CS"     "$M_CS"
printf '  %-14s | %-42.42s | %-42.42s\n' codeHash "${F_CH:0:42}" "${M_CH:0:42}"
printf '  %-14s | %-42s | %-42s\n' storageCount "$F_SC"     "$M_SC"
echo

# Fail-loud if the two dumps aren't at the same height — otherwise any
# comparison below is meaningless.
if [[ "$F_H" != "$M_H" ]]; then
    echo "ERROR: height mismatch (flatkv=$F_H, memiavl=$M_H); pick a --height both backends have." >&2
    exit 3
fi

HEADER_OK=1
[[ "$F_NONCE" == "$M_NONCE" ]] || HEADER_OK=0
[[ "$F_CH"    == "$M_CH"    ]] || HEADER_OK=0
[[ "$F_CS"    == "$M_CS"    ]] || HEADER_OK=0
[[ "$F_SC"    == "$M_SC"    ]] || HEADER_OK=0

# ---- storage-slot set diff (streaming; safe on 8 GB dumps) ----
# Do NOT use jq here: jq builds a whole-file AST and blows up the machine
# on multi-GB dumps. The dumps emitted by `seidb {flatkv,iavl}-account`
# are already key-sorted (Go's json encoder sorts map keys, and we
# explicitly rebuild the map sorted too), so we can extract (slot, value)
# as a TSV with grep+sed in a single linear pass and use `comm` / `join`
# directly — no external sort, no in-memory parse.
#
# The canonical pretty-JSON form of a storage entry is:
#     "0xHEX...": "0xHEX...",
# with exactly 4 leading spaces, regardless of dump size.
echo "  computing slot-set diff (streaming; no jq)..."
F_SLOTS="$OUT_DIR/slots.flatkv.tsv"
M_SLOTS="$OUT_DIR/slots.memiavl.tsv"

extract_slots_tsv() {
    # Emit one "slot<TAB>value" line per storage entry. Matches the exact
    # line shape produced by encoding/json with "  " indent at top+map
    # nesting (= 4 leading spaces for each storage entry).
    grep -oE '^    "0x[0-9a-f]+": "0x[0-9a-f]+"' "$1" \
      | sed -E 's/^    "(0x[0-9a-f]+)": "(0x[0-9a-f]+)"/\1\t\2/'
}

extract_slots_tsv "$FLATKV_JSON"  > "$F_SLOTS"
extract_slots_tsv "$MEMIAVL_JSON" > "$M_SLOTS"

comm -23 <(cut -f1 "$F_SLOTS") <(cut -f1 "$M_SLOTS") > "$OUT_DIR/slots_only_in_flatkv.txt"
comm -13 <(cut -f1 "$F_SLOTS") <(cut -f1 "$M_SLOTS") > "$OUT_DIR/slots_only_in_memiavl.txt"

# Slots present on both sides but with different values — the real
# write-path drift signal.
join -t$'\t' "$F_SLOTS" "$M_SLOTS" \
    | awk -F'\t' '$2 != $3 { printf "%s\tflatkv=%s\tmemiavl=%s\n", $1, $2, $3 }' \
    > "$OUT_DIR/slots_value_mismatch.txt"

F_ONLY=$(wc -l < "$OUT_DIR/slots_only_in_flatkv.txt")
M_ONLY=$(wc -l < "$OUT_DIR/slots_only_in_memiavl.txt")
VAL_DIFF=$(wc -l < "$OUT_DIR/slots_value_mismatch.txt")

printf '  slot-set diff: only_flatkv=%s  only_memiavl=%s  value_mismatch=%s\n' \
    "$F_ONLY" "$M_ONLY" "$VAL_DIFF"
echo "  (see $OUT_DIR/slots_*.txt; first 5 of each shown below)"
for f in slots_only_in_flatkv.txt slots_only_in_memiavl.txt slots_value_mismatch.txt; do
    if [[ -s "$OUT_DIR/$f" ]]; then
        echo "  --- $f (head) ---"
        head -n 5 "$OUT_DIR/$f" | sed 's/^/    /'
    fi
done

# ---- optional line-diff, only when both files are small ----
F_BYTES=$(stat -c%s "$FLATKV_JSON"  2>/dev/null || stat -f%z "$FLATKV_JSON")
M_BYTES=$(stat -c%s "$MEMIAVL_JSON" 2>/dev/null || stat -f%z "$MEMIAVL_JSON")
if (( F_BYTES <= MAX_SMALL_DIFF_BYTES && M_BYTES <= MAX_SMALL_DIFF_BYTES )); then
    if [[ $HEADER_OK -eq 1 && "${F_ONLY:-0}" == "0" && "${M_ONLY:-0}" == "0" && "${VAL_DIFF:-0}" == "0" ]]; then
        echo "OK: FlatKV and memIAVL agree on $ADDRESS at height=$F_H"
        [[ $QUIET -eq 0 ]] && { echo "--- dump ---"; head -c 2000 "$FLATKV_JSON"; echo; }
        [[ $CLEANUP_ON_MATCH -eq 1 ]] && rm -rf "$OUT_DIR"
        exit 0
    fi
    echo "--- unified diff (flatkv → memiavl) ---"
    diff -u "$FLATKV_JSON" "$MEMIAVL_JSON" || true
else
    echo "  (line-diff skipped: one side > $MAX_SMALL_DIFF_BYTES bytes — see slot files above)"
fi

echo "--- raw dumps retained at $OUT_DIR ---"
exit 1
