#!/usr/bin/env bash
# Assert every .goreleaser.yaml before-hook can actually be executed.
#
# GoReleaser execs a hook directly rather than through a shell, so argv[0] must be a
# real program on PATH: a bare `VAR=value cmd` prefix is taken as the program name and
# the whole before pipe aborts, publishing a tag with no artefacts. Nothing else in CI
# runs goreleaser, so without this check every hook is an unexecuted string until a tag
# is pushed.
set -euo pipefail

CONFIG="${1:-.goreleaser.yaml}"

# mapfile is bash 4+, absent on the macOS bash a developer runs this with, so read the
# list into a temp file and loop over it instead.
hooks_file=$(mktemp)
trap 'rm -f "$hooks_file"' EXIT
python3 -c '
import sys, yaml
cfg = yaml.safe_load(open(sys.argv[1]))
for h in (cfg.get("before") or {}).get("hooks") or []:
    # hooks are either a plain string or {cmd: ..., env: [...]}
    print(h if isinstance(h, str) else h.get("cmd", ""))
' "$CONFIG" > "$hooks_file"

count=$(grep -c . "$hooks_file" || true)
if [ "$count" -eq 0 ]; then
  echo "check-goreleaser-hooks: no before hooks found in $CONFIG" >&2
  exit 1
fi

failed=0
while IFS= read -r hook; do
  [ -n "$hook" ] || continue
  # shellcheck disable=SC2086 # deliberate: split exactly as goreleaser's shellwords does
  set -- $hook
  prog=$1
  if command -v "$prog" >/dev/null 2>&1; then
    printf '  ok    %-8s  %s\n' "$prog" "$hook"
  else
    printf '  FAIL  %-8s  %s\n' "$prog" "$hook"
    case "$prog" in
      *=*) echo "        argv[0] is an environment assignment. GoReleaser does not use a shell;" >&2
           echo "        prefix the command with 'env' instead." >&2 ;;
      *)   echo "        argv[0] is not on PATH." >&2 ;;
    esac
    failed=1
  fi
done < "$hooks_file"

[ "$failed" -eq 0 ] || { echo "check-goreleaser-hooks: at least one hook cannot be executed" >&2; exit 1; }
echo "check-goreleaser-hooks: all $count before hooks resolve to an executable"
