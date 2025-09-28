# Nova Operator Handbook

## 1. Overview
Nova automates compounding for Sei validators by orchestrating reward withdrawal, validator
scoring, risk evaluation, and delegation broadcasts. This handbook outlines the operational
procedures required to run Nova in production environments.

## 2. Prerequisites
- Python 3.11+
- Access to a Sei RPC endpoint with sufficient rate limits
- Vault or hardware wallet credentials
- Telegram bot token and chat ID for alerts

## 3. Installation
```bash
./install.sh
```
The script installs dependencies, sets up a Python virtual environment, and registers the `nova`
CLI as an editable package.

## 4. Configuration Profiles
Profiles reside in `config/profiles/`. Duplicate the sample file and adjust wallet addresses,
validator sets, scheduling cadence, and alert routing. Validate the profile with:
```bash
python -m nova.tools.validate_config --profile config/profiles/your-profile.yaml
```

## 5. Dry Runs
Execute a dry run before mainnet activation:
```bash
nova run --profile config/profiles/your-profile.yaml --dry-run
```
Inspect the logs to ensure risk checks and validator ordering meet expectations.

## 6. Production Run
Launch Nova in continuous mode:
```bash
nova auto --profile config/profiles/your-profile.yaml
```
Monitor scheduler logs and ensure alerts are received after each delegation.

## 7. Dashboard
Start the dashboard service:
```bash
python -m nova.ui.dashboard.app --profile config/profiles/your-profile.yaml
```
Access the UI at `http://localhost:8000` to view wallet metrics and live logs.

## 8. Upgrades
1. Pull the latest release
2. Re-run `./install.sh`
3. Re-validate configuration
4. Restart Nova services (CLI, systemd, or Kubernetes deployment)

## 9. Incident Response
- **Vault Failure**: switch signer to `local` using the failover wallet while the secure signer is
  restored.
- **RPC Instability**: throttle the scheduler by increasing interval or temporarily disabling
  automation via `nova auto` stop.
- **Validator Underperformance**: adjust the validator allow list in the profile and trigger a
  manual compound run to redeploy stake.

## 10. Contact
For additional support, open an issue or contact the Nova operator channel.
