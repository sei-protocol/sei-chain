# Codex AI Governance Scribe

`aiScribe.mjs` is the command-line orchestrator for the "full AI governance rewrite". It analyses reward indices, backlog risk,
and emission schedules to generate an actionable governance plan for Sei reward markets.

## Features

- Pulls live reward backlog data from the `MultiRewardDistributor` (when not in `--offline` mode).
- Correlates market metadata (baseline emissions, utilization targets, backlog thresholds) from a JSON config file.
- Forecasts emission coverage across configurable horizons and highlights systemic shortfalls.
- Generates per-market operational playbooks (NodeBot call flows) and governance proposal scaffolds.
- Supports text, Markdown, or JSON output for downstream automation and reporting.

## Usage

```bash
# Install dependencies (from this directory)
npm install

# Generate a plan using the example config (offline seed data only)
node aiScribe.mjs --offline --format markdown

# Run against live chain data
RPC_URL=https://sei.example.org \
REWARD_DISTRIBUTOR=0xYourDistributor \
node aiScribe.mjs \
  --config ./templates/governance-config.example.json \
  --accounts 0xYourAccount,0xAnotherAccount \
  --forecast-horizon 45 \
  --format text \
  --export ./plan.txt
```

Environment variables mirror the CLI flags (`GOVERNANCE_CONFIG`, `GOVERNANCE_FORMAT`, etc.) so the tool can be dropped into
cron jobs or the NodeBot agent.

## Configuration Schema

The config file (see [`templates/governance-config.example.json`](./templates/governance-config.example.json)) provides:

- **`network`**: metadata for display.
- **`distributor`**: default contract address (overridden by CLI/env when needed).
- **`rewardToken`**: symbol + decimals for formatting and math.
- **`accounts`**: optional default account list when CLI `--accounts` is omitted.
- **`global`**: systemic thresholds (buffer days, max backlog before emergency sync).
- **`emissionSchedule`**: rolling emission projections (per-day totals over epoch windows).
- **`markets`**: array of market descriptors, each with optional `borrow`/`supply` stream configs:
  - `baselineEmissionPerDay`: expected emissions per stream (numeric or string, token units).
  - `maxBacklogDays`: threshold for alerting/escalation.
  - `seedOutstanding`: fallback backlog estimate for offline planning.
  - `boostOnUnderUtilization`: flag to auto-suggest emission boosts when utilization is below target.
  - `notes`: extra guidance for the generated report.

## Output

Depending on the format you choose, `aiScribe` returns:

- **text**: console-friendly action report with per-market breakdowns.
- **markdown**: drop-in documentation for governance calls or dashboards.
- **json**: structured data for further automation (e.g. feeding into bots or dashboards).

Each recommendation contains severity, operational steps, and governance scaffolding so that human reviewers (or autonomous
agents) can immediately execute the plan.
