#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${1:-$PWD}"
BUNDLE_DIR="$ROOT_DIR/bundle"
SIGHT_DIR="$BUNDLE_DIR/sightings"

mkdir -p "$SIGHT_DIR"

# Helper to write a heredoc safely
write_file() {
  local path="$1"; shift
  mkdir -p "$(dirname "$path")"
  cat >"$path" <<'EOF'
$CONTENT_PLACEHOLDER
EOF
}

# Because we're shipping this as a JSON blob, we replace placeholders with jq later.
# We'll embed each file's content using jq to avoid escaping nightmares.

if ! command -v jq >/dev/null 2>&1; then
  echo "[!] jq not found. Install: sudo apt-get update && sudo apt-get install -y jq" >&2
  exit 1
fi

# Extract this JSON (stdin) to files
# Usage: cat BIG.json | scripts/bundle_build.sh

JSON_INPUT="$(cat)"

# Write files
mapfile -t FILE_KEYS < <(echo "$JSON_INPUT" | jq -r '.files | keys[]')
for key in "${FILE_KEYS[@]}"; do
  content=$(echo "$JSON_INPUT" | jq -r --arg k "$key" '.files[$k].content')
  outpath="$ROOT_DIR/$key"
  mkdir -p "$(dirname "$outpath")"
  printf "%s" "$content" > "$outpath"
  # chmod if mode present
  mode=$(echo "$JSON_INPUT" | jq -r --arg k "$key" '.files[$k].mode // empty')
  if [[ -n "$mode" ]]; then
    chmod "$mode" "$outpath"
  fi
  echo "[+] wrote $outpath"
done

# Optional PDF build if pandoc present
if command -v pandoc >/dev/null 2>&1; then
  echo "[*] pandoc found — generating PDF"
  (cd "$BUNDLE_DIR" && pandoc CLAIM_SUMMARY.md -o CLAIM_SUMMARY.pdf)
  echo "[+] wrote $BUNDLE_DIR/CLAIM_SUMMARY.pdf"
else
  echo "[i] pandoc not found — skip PDF (install: sudo apt-get update && sudo apt-get install -y pandoc)"
fi

# Hash sealing
(cd "$BUNDLE_DIR" && sha256sum sovereign_index.json txids.csv sightings/txlog.json CLAIM_SUMMARY.md > SHASUMS256.txt)

# Show verify commands
echo "\n[verify] Run these on your node host (example LCD):"
echo "python3 scripts/sei_tx_fetch.py --hash 4ee194ba272c3ece2bcd30be170373cf9a6cdd5cf648ae44e7b181ca223a8b3a --lcd http://localhost:1317"
echo "python3 scripts/sei_tx_fetch.py --hash 75cea32eb2504a699e2b076d7794219d994572ab1848cfa8582e8ef2601be933 --lcd http://localhost:1317"

# Git helper (optional)
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "\n[git] Staging bundle/ for commit"
  git add bundle
  echo "[git] Next: git commit -m 'feat(attribution): add x402 • Sei settlement evidence bundle' && git push"
fi

# done
echo "\n[done] Bundle materialized at $BUNDLE_DIR"