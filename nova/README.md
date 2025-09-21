# Nova – Autonomous SEI Capital Operator

Nova is a validator-class compounding platform for the Sei network. It provides a modular Python
stack for orchestrating secure reward harvesting, validator scoring, risk-aware redelegation, and
operator observability. The repository is organized as a mono-repo so that wallet custody, policy
logic, automation, and deployment concerns remain isolated but interoperable.

## Key Capabilities

- **Smart Compounding Engine** – deterministic withdraw → analyze → allocate pipelines with
  dry-run support and telemetry hooks.
- **Yield Intelligence** – pluggable validator scoring backed by configurable data sources,
  historical caching, and machine-learning ready feature extraction.
- **Risk Governance** – policy-driven buffer management, validator caps, and safety stop
  triggers that can be validated via simulation harnesses.
- **Operator Control Planes** – CLI, REST API, and HTMX-based dashboard for manual oversight and
  alert acknowledgement.
- **Secure Wallet Abstractions** – Vault adapter, local keyring bridge, and external signer hooks
  for hardware or multi-sig workflows.
- **Infrastructure Ready** – Docker, Kubernetes, and systemd deployment artifacts plus install
  scripts for bare-metal bootstrapping.

## Getting Started

1. Install dependencies via the helper script:

   ```bash
   ./install.sh
   ```

2. Populate the configuration at `config/profiles/pacific-1.validator.yaml` or create a new
   profile with wallet details, validator targets, and alert routing.

3. Run a dry-run of the compounding engine:

   ```bash
   nova run --profile pacific-1.validator --dry-run
   ```

4. Launch the dashboard:

   ```bash
   nova ui serve --profile pacific-1.validator
   ```

Refer to `docs/OPERATOR_HANDBOOK.md` for full operational guidance.

## Repository Layout

```
core/         # Orchestration and scheduling logic
strategies/   # Validator scoring and allocation strategies
wallet/       # Signer and custody integrations
logic/        # Risk, policy, and analytics modules
api/          # REST server and auth utilities
ui/           # Dashboard templates and assets
alerts/       # Notification providers
infra/        # Docker/Kubernetes/systemd assets
config/       # Schemas and runtime profiles
scripts/      # Installers and utility scripts
```

## License

Nova is released under the Apache 2.0 license. See `LICENSE` for details.
