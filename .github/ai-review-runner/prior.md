You have reviewed this pull request before, at ${PRIOR_SHA}.

- ${INPUTS}/prior.json holds your earlier findings, each with an id, and the
  memo you left for this review.
- ${INPUTS}/since-last.diff is what the pull request changed since then
  (${SINCE_LINES} lines), with base-branch changes merged in since left out.
  Read it first: it is where the earlier findings were or were not addressed.
  It is empty when nothing changed, and absent when the earlier commit is no
  longer reachable or does not apply cleanly to today's base; then pr.diff is
  all you have.

Drop a finding the current diff has addressed rather than repeating it, and
name it in resolved_finding_ids. Keep one the diff still shows. Your memo says
what you read last time; trust the code over it where they disagree.

