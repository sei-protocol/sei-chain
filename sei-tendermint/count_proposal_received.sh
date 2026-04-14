#!/usr/bin/env bash
set -euo pipefail

if (($# == 0)); then
  set -- /dev/stdin
fi

awk '
function unquote(value) {
  if (value ~ /^".*"$/) {
    return substr(value, 2, length(value) - 2)
  }
  return value
}

function field_value(line, key,    pattern, value) {
  pattern = key "=(\"[^\"]*\"|[^[:space:]]+)"
  if (match(line, pattern)) {
    value = substr(line, RSTART + length(key) + 1, RLENGTH - length(key) - 1)
    return unquote(value)
  }
  return ""
}

function print_report(    proposer, total, expected, unexpected, width, i) {
  print "Totals"
  print "  total: " counts["total"]
  print "  expected: " counts["expected"]
  print "  unexpected: " counts["unexpected"]
  print ""
  print "By proposer"

  width = 50
  printf "%-" width "s %8s %10s %12s\n", "proposer", "total", "expected", "unexpected"
  for (i = 0; i < width + 34; i++) {
    printf "-"
  }
  printf "\n"

  PROCINFO["sorted_in"] = "@ind_str_asc"
  for (proposer in proposers) {
    total = proposer_counts[proposer, "total"] + 0
    expected = proposer_counts[proposer, "expected"] + 0
    unexpected = proposer_counts[proposer, "unexpected"] + 0
    printf "%-" width "s %8d %10d %12d\n", proposer, total, expected, unexpected
  }

  if (skipped_count > 0) {
    print ""
    printf "Skipped %d matching lines without both proposer and expected fields:\n", skipped_count
    limit = skipped_count < 20 ? skipped_count : 20
    for (i = 1; i <= limit; i++) {
      print "  " skipped[i]
    }
    if (skipped_count > 20) {
      printf "  ... and %d more\n", skipped_count - 20
    }
  }
}

index($0, "proposal received") || index($0, "received proposal") {
  height = field_value($0, "height")
  round = field_value($0, "round")
  proposer = field_value($0, "proposer")
  expected = field_value($0, "expected")

  if (height == "" || round == "" || proposer == "" || expected == "") {
    skipped_count++
    skipped[skipped_count] = FILENAME ":" FNR
    next
  }

  dedup_key = height SUBSEP round SUBSEP proposer SUBSEP expected
  if (seen[dedup_key]++) {
    next
  }

  status = proposer == expected ? "expected" : "unexpected"
  counts["total"]++
  counts[status]++
  proposers[proposer] = 1
  proposer_counts[proposer, "total"]++
  proposer_counts[proposer, status]++
}

END {
  print_report()
}
' "$@"
