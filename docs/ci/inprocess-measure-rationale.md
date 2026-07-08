# In-process measurement job — rationale (decision-support, not productionization)

Additive, non-gating CI job (`.github/workflows/inprocess-measure.yml`) that runs
a representative subset of the in-process suites alongside the docker matrix and
emits CI-measured numbers. Its only purpose: prove or disprove the in-process
payoff on real CI-to-CI data before funding deeper work (PLT-781). It does not
gate merges and must be kept out of branch protection's required checks.

## What it answers

The tracker's before/after is directional only — local `go test` wall-clock vs
docker CI job wall-clock, different runners. This job removes the runner confound:
same `ubuntu-large` class as the docker legs, so runner-minutes are comparable.

Emitted per shard: per-suite wall-clock, setup seconds (compile + `npm ci` — the
honest analog of docker's serial ~9-10 min prepare-cluster build, measured
separately so it is not hidden inside the test number), total test-exec seconds,
and pass/flake over N runs (default 3).

## Shards

Representative, not exhaustive — the full in-process package exceeds a single
40-min `go test`, so a subset is the right measurement unit.

| Shard | Package | Covers |
|---|---|---|
| `tier1-yaml` | `integration_test/runner` | Tier-1 shared-net YAML (bank, distribution, staking, oracle, authz, wasm, seidb) |
| `tier1-evm` | `integration_test/runner` | Tier-C hardhat driver (compat, precompile, endpoints, rpc.io) |
| `localnode-n4` | `integration_test/runner_localnode` | N=4 localnode gov-net (mint, startup, gov) |
| `giga` | `integration_test/runner_giga` | pinned-giga single-mode |

Each shard is a separate `go test` process with a DISTINCT `SEI_INPROCESS_PORT_BASE`
(20000/21000/22000/23000 — in `[1024, 32512)`, >256 apart), which the port-base
hardening validates. In CI each leg is its own host, so the base primarily removes
the intra-process TOCTOU; distinct-per-leg bases future-proof packing shards onto
one runner.

## Not covered here (branch-separate)

The Tier-2 subprocess operational suites (crash-recovery, snapshot, state-sync,
upgrade) and GIGA-Mixed live on `feat/inprocess-subprocess-hypervisor`, not on
phaseA. A `tier2` shard lands here once that branch converges into the phaseA line.

## Honesty bounds

- Compute (runner-minutes): this job makes it CI-to-CI for the subset.
- Wall-clock on the merge critical path: this job is ADDITIVE, so it ADDS
  wall-clock. It does not build the docker images or shorten the longest docker
  leg. A wall-clock win exists only if in-process REPLACES docker legs and the
  runner pool is the bottleneck — unproven, and only measurable once the pool
  contention under a full parallel matrix is observed.
