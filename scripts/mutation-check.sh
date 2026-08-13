#!/usr/bin/env bash
#
# Break a line of code on purpose and report which test noticed.
#
# A test suite that passes says nothing about whether it would fail. This answers the other
# question: take a claim the code makes, remove it, and name the test that objects. A mutation
# nothing objects to means either the claim is untested or the mutation changed no behaviour, and
# those two are different findings that need saying apart.
#
# Usage:
#   scripts/mutation-check.sh <file> <old-text-file> <new-text-file> <package>...
#
# The old and new text come from files rather than arguments, so a patch carrying quotes, tabs or
# newlines survives the shell.
#
# Five properties this enforces, each because getting it wrong reports a false result:
#
#   1. The baseline is green first. A suite already failing reports every mutation as caught.
#   2. The mutation applied. A stale anchor silently tests the unmutated code and reports a pass
#      as a survived mutation.
#   3. Compile status comes from the compiler. Deciding it by grepping test output for phrases
#      like "cannot use" misreads a test whose own failure message contains that phrase, which is
#      exactly how a real catch gets reported as a build error.
#   4. The file is restored, and the restore is verified green. An unrestored mutation silently
#      poisons every later run.
#   5. A surviving mutation is reported as needing a decision, never as a pass. It is either an
#      untested claim or an equivalent mutant, and only a human can say which.

set -uo pipefail

if [[ $# -lt 4 ]]; then
    sed -n '3,26p' "$0" | sed 's|^# \{0,1\}||'
    exit 2
fi

target=$1
old_file=$2
new_file=$3
shift 3
packages=("$@")

for f in "$target" "$old_file" "$new_file"; do
    [[ -r "$f" ]] || { echo "cannot read $f" >&2; exit 2; }
done

backup=$(mktemp)
cleanup() {
    cp "$backup" "$target"
    gofmt -w "$target" >/dev/null 2>&1 || true
    rm -f "$backup"
}
trap cleanup EXIT

compiles() { go vet "${packages[@]}" >/dev/null 2>&1; }

failing_tests() {
    go test "${packages[@]}" -count=1 2>&1 |
        grep -E '^--- FAIL' | sed 's/--- FAIL: //; s/ .*//' | sort -u | tr '\n' ' '
}

# 1. The baseline is green, or every result below is meaningless.
cp "$target" "$backup"
if ! compiles; then
    echo "BASELINE DOES NOT COMPILE — fix that before measuring anything" >&2
    exit 1
fi
if [[ -n "$(failing_tests)" ]]; then
    echo "BASELINE IS ALREADY FAILING ($(failing_tests))— a mutation cannot be measured against it" >&2
    exit 1
fi

# 2. The mutation applied, or the run below tests the unmutated code.
if ! python3 - "$target" "$old_file" "$new_file" <<'PY'
import sys
target, old_file, new_file = sys.argv[1:4]
body = open(target).read()
old = open(old_file).read()
new = open(new_file).read()
if old not in body:
    sys.stderr.write("the text to replace is not in %s; the anchor has moved\n" % target)
    sys.exit(1)
if old == new:
    sys.stderr.write("the replacement is identical to the original; nothing would be mutated\n")
    sys.exit(1)
open(target, "w").write(body.replace(old, new, 1))
PY
then
    echo "MUTATION DID NOT APPLY" >&2
    exit 1
fi
gofmt -w "$target" >/dev/null 2>&1 || true

# 3. Compile status from the compiler, never from test output.
if ! compiles; then
    echo "INVALID MUTATION — it does not compile, so it measures nothing"
    exit 0
fi

caught=$(failing_tests)

# 4 and 5. Restore, verify the restore, then report.
cleanup
trap - EXIT
if ! compiles || [[ -n "$(failing_tests)" ]]; then
    echo "RESTORE FAILED — $target is not back to a green state" >&2
    exit 1
fi

if [[ -n "$caught" ]]; then
    echo "CAUGHT by: $caught"
else
    echo "SURVIVED — decide which this is:"
    echo "  an untested claim, and a test is owed; or"
    echo "  an equivalent mutant, which changed no observable behaviour and needs saying so"
    exit 3
fi
