#!/usr/bin/env bash
#
# Run `go test "$@"` with CI-friendly output.
#
# With -coverpkg spanning the whole module, every package result line ends in
# "coverage: X% of statements in <every package in the module>", which makes
# each line tens of kilobytes long and buries failures. This trims that list
# and, when tests fail, repeats the failed packages, tests and panics at the
# end of the log, as error annotations, and in the job summary.
set -uo pipefail

log=$(mktemp)
trap 'rm -f "$log"' EXIT

go test "$@" 2>&1 | sed -uE 's/(coverage: [^ ]+ of statements) in .*/\1/' | tee "$log"
status=${PIPESTATUS[0]}
if ((status == 0)); then
  exit 0
fi

# Without -v, go test prints each package's output as one block ending in its
# result line, so failed tests and panics are attributed to the next FAIL line.
failures=$(awk '
  /^(ok|FAIL|\?)[ \t]+[^ \t]/ || /^\t[^ \t]+\t+coverage:/ {
    if ($1 == "FAIL") {
      sub(/^FAIL[ \t]+/, "")
      print "FAIL " $0
      printf "%s", tests
    }
    tests = ""
    next
  }
  /^ *--- FAIL: / || /^panic: / { tests = tests "    " $0 "\n" }
' "$log")

if [[ -z "$failures" ]]; then
  failures="go test exited with status $status; no FAIL lines found, see the log above."
fi

echo
echo "==================== Failures ===================="
echo "$failures"

grep '^FAIL ' <<<"$failures" | while read -r _ pkg _; do
  echo "::error title=go test failed::$pkg"
done

if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
  {
    echo "### go test failures"
    echo '```'
    echo "$failures"
    echo '```'
  } >>"$GITHUB_STEP_SUMMARY"
fi

exit "$status"
