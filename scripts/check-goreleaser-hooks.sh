#!/usr/bin/env bash
# Assert every .goreleaser.yaml hook can actually be executed.
#
# GoReleaser execs a hook directly rather than through a shell, so argv[0] must be a real
# program on PATH: a bare `VAR=value cmd` prefix is taken as the program name and the
# whole before pipe aborts, publishing a tag with no artefacts. Nothing else in CI runs
# goreleaser, so without this check every hook is an unexecuted string until a tag is
# pushed.
set -euo pipefail

CONFIG="${1:-.goreleaser.yaml}"

python3 -c 'import yaml' 2>/dev/null || {
  echo "check-goreleaser-hooks: needs python3 with PyYAML (pip install pyyaml)" >&2
  exit 1
}

# mapfile is bash 4+, absent on the bash macOS ships, so collect into a file and loop.
entries=$(mktemp)
trap 'rm -f "$entries"' EXIT
python3 -c '
import sys, yaml
cfg = yaml.safe_load(open(sys.argv[1])) or {}
for h in (cfg.get("before") or {}).get("hooks") or []:
    # a hook is either a plain string or {cmd: ..., env: [...]}
    print(h if isinstance(h, str) else h.get("cmd", ""))
# builds[].tool is exec'"'"'d the same way, so it belongs to the same class.
for b in cfg.get("builds") or []:
    if b.get("tool"):
        print(b["tool"])
' "$CONFIG" > "$entries"

count=$(grep -c . "$entries" || true)
[ "$count" -gt 0 ] || { echo "check-goreleaser-hooks: no hooks found in $CONFIG" >&2; exit 1; }

fail () { printf '  FAIL  %s\n' "$1"; shift; for line in "$@"; do echo "        $line" >&2; done; failed=1; }

failed=0
# Word-splitting here stands in for goreleaser's shellwords. Disable globbing so a hook
# containing * is not expanded against the working directory; quoting is not reproduced,
# which is fine for the simple hooks this config uses.
set -f
while IFS= read -r hook; do
  [ -n "$hook" ] || continue
  # shellcheck disable=SC2086 # deliberate split, see above
  set -- $hook

  # `env` moves the real program along, so resolve past it and the VAR=value operands it
  # consumes. Without this the one hook that needs `env` is the one hook not checked.
  if [ "$1" = "env" ] || [ "$1" = "/usr/bin/env" ]; then
    shift
    while [ $# -gt 0 ]; do
      case "$1" in *=*) shift ;; *) break ;; esac
    done
    [ $# -gt 0 ] || { fail "$hook" "env is given no command to run."; continue; }
  fi

  prog=$1
  if ! command -v "$prog" >/dev/null 2>&1; then
    case "$prog" in
      *=*) fail "$hook" "argv[0] is an environment assignment. GoReleaser does not use a shell;" \
                        "prefix the command with 'env' instead." ;;
      *)   fail "$hook" "argv[0] ($prog) is not on PATH." ;;
    esac
    continue
  fi

  # An interpreter proves only that the interpreter exists, so check the script it runs.
  script=""
  case "$prog" in
    bash|sh|/bin/bash|/bin/sh)
      shift
      while [ $# -gt 0 ]; do
        case "$1" in -*) shift ;; *) script=$1; break ;; esac
      done
      ;;
    ./*|scripts/*) script=$prog ;;
  esac
  if [ -n "$script" ] && [ ! -r "$script" ]; then
    fail "$hook" "runs '$script', which does not exist or is not readable."
    continue
  fi

  printf '  ok    %-34s %s\n' "$prog${script:+ -> $script}" "$hook"
done < "$entries"
set +f

[ "$failed" -eq 0 ] || { echo "check-goreleaser-hooks: at least one hook cannot be executed" >&2; exit 1; }
echo "check-goreleaser-hooks: all $count hooks resolve to an executable program and script"
