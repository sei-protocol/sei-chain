# Review guidelines for AI agents

Repo-specific conventions for automated PR review (Codex, Cursor, Claude, and
any other AI reviewer). The two patterns below look like bugs in isolation
but are normal consequences of how Sei cuts releases and rolls out upgrades.
Do not report them as findings unless a concrete additional signal (see the
caveats under each) makes them real.

## 1. Referencing a not-yet-created release tag is expected

Sei only creates a new release tag — and registers it in `app/tags` with a
handler in `app/upgrades.go` — *after* the code that depends on it has
already been merged to main, never before. So it is completely normal for a
PR to introduce a version-gated constant (e.g. `const FooUpgrade = "v6.7"`)
that:

- does not yet appear anywhere in `app/tags`,
- has no corresponding upgrade handler in `app/upgrades.go`, and
- therefore cannot currently be produced by `ctx.ClosestUpgradeName()` /
  `ctx.LatestUpgrade()`.

Do **not** flag this as "the tag/handler is never registered," "this branch
is permanently unreachable/dead," or "this constant doesn't match anything
in the tree." The tag is cut and the handler wired up in a follow-up step
after this PR lands, using the same version string, following the existing
`app/tags` naming convention.

This only becomes a real finding when:
- the PR description or diff explicitly claims *this* PR adds the tag/handler
  (i.e. it's the release-cut PR) and it doesn't, or
- the referenced version string doesn't follow Sei's tag naming convention
  (compare against the existing entries in `app/tags`).

## 2. Version-gated logic and block/state sync: don't assume cross-version execution

Do not flag scenarios along the lines of "new code could process a
pre-upgrade height and diverge from what was originally committed," or
"this upgrade gate can never activate because the upgrade hasn't run yet,"
as correctness bugs — unless the diff itself has a concrete logic bug in the
gate (e.g. an inverted comparison, wrong field, off-by-one on the height).

Operationally, a given binary is never used to execute or re-execute a
height range that predates its own earliest registered upgrade. Block/state
sync always proceeds version-by-version: e.g. if a node's target height
spans releases v6.0 and v6.1, the node first syncs with the v6.0 binary up
to v6.1's upgrade height, halts, switches to the v6.1 binary, and continues
syncing from there. New code never processes old, not-yet-upgraded state.

Consequences for review:
- Height/upgrade-name gates (`ctx.ClosestUpgradeName()`, `ctx.IsTracing()`,
  semver comparisons against an upgrade constant, etc.) do not need extra
  defenses against "a newer binary ran against pre-upgrade state" — that
  situation does not occur in Sei's deployment model.
- By the time a binary is live (or tracing/replaying) at or after its own
  upgrade height, that upgrade's handler has necessarily already applied on
  that node, so upgrade-gated branches are reachable for those blocks. Don't
  call them "permanently unreachable" solely because the tag/handler isn't
  registered yet at PR-review time — see §1.

If you believe a version gate is genuinely broken on its own logic (wrong
comparison direction, wrong constant, wrong context field), still report
it — this guidance only rules out the "the tag doesn't exist yet" and
"old code might run against post-upgrade state" false positives.
