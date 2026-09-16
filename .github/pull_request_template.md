<!--
Guidance for AI agents and humans writing this description.

Goal: a reader who has not seen the diff should understand, in under a
minute, what this PR changes, why, and what could go wrong. Optimise for the
reader's cognitive cost, not for completeness.

Write plain prose. No headings, no bullet-list recap of the diff, no
"Testing performed" section. Two to four short paragraphs is the target.

Paragraph 1: the problem or motivation. State the observed behaviour or
gap and why it matters. Link the issue if there is one.

Paragraph 2: the change. Explain the approach and the key decision(s),
not the file list. Name concrete symbols (`Keeper.SetNonce`, `TxMempool`)
so readers can jump into the diff. If there is a non-obvious alternative
you rejected, say why in one sentence.

Paragraph 3 (when relevant): risk and rollout. Consensus, state, or
wire-format impact; migration or upgrade-handler needs; feature gating;
what a reviewer should scrutinise most closely. Say in one sentence how
you validated the change (which tests or manual runs), without pasting
output.

Omit anything a reader can trivially see in the diff. Do not restate
commit messages. Do not include agent/session links or attribution.

Keep this description current. Every time you push to this branch,
re-read the description against the full diff (`git diff origin/main...HEAD`)
and update it so that it still describes the PR as it is now, not as it
was first opened. Remove or rewrite paragraphs that review feedback has
made stale. Replace this template text entirely; do not leave any of
these comments or any placeholder in the final description.
-->
