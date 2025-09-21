# SEI Auto-Compounder Bot (Full Max Mode)

This utility automates compounding of SEI staking rewards and comes with telemetry, alerting, and optional dashboard support. It is intended for validators and power users who need a resilient, 24/7 restaking companion.

## Features

- Automated withdraw + delegate loop with configurable sleep interval
- Multi-validator support with weighted and round-robin strategies
- Automatic RPC failover across a configurable endpoint list
- Structured logging with rotation
- Telegram alerts for success and failure cases
- Optional Flask dashboard for live monitoring
- Dockerfile and `run.sh` helper script for quick deployment

## Project Layout

```
sei-compounder-bot/
├── bot/
│   ├── __init__.py
│   ├── config.yaml
│   ├── main.py
│   ├── telegram.py
│   ├── utils.py
│   ├── validators.py
│   └── logs/
│       └── .gitkeep
├── dashboard/
│   ├── app.py
│   └── templates/
│       └── status.html
├── Dockerfile
├── README.md
├── requirements.txt
└── run.sh
```

## Quick Start

1. **Install dependencies**

   ```bash
   python3 -m venv venv
   source venv/bin/activate
   pip install -r requirements.txt
   ```

2. **Configure** the bot by editing `bot/config.yaml`. Provide your wallet key name, RPC endpoints, Telegram credentials, and validator list. Weighted validator selection is supported via entries such as:

   ```yaml
   validators:
     - address: seivaloper1...
       weight: 2
     - address: seivaloper1...
       weight: 1
   validator_strategy: weighted
   ```

   Supported strategies are `weighted` (default), `round_robin`, and `random`.

3. **Run the bot**

   ```bash
   ./run.sh
   ```

   Use `./run.sh --once` for a single compounding cycle.

4. **Optional dashboard**

   ```bash
   export FLASK_APP=dashboard/app.py
   flask run --port 8080
   ```

   Visit `http://localhost:8080` to view recent logs.

## Docker Deployment

```
docker build -t sei-compounder .
docker run -d --restart unless-stopped --name sei_bot \
  -v $PWD/bot/config.yaml:/app/bot/config.yaml \
  -v $PWD/bot/logs:/app/bot/logs \
  sei-compounder
```

## Systemd Service

Create `/etc/systemd/system/sei-compounder.service`:

```
[Unit]
Description=SEI Compounder Bot
After=network.target

[Service]
WorkingDirectory=/opt/sei-compounder-bot
ExecStart=/usr/bin/python3 -m bot.main
Restart=always
User=ubuntu

[Install]
WantedBy=multi-user.target
```

Reload and enable the service:

```
sudo systemctl daemon-reload
sudo systemctl enable --now sei-compounder
```

## Alerts

Set `telegram.enabled: true` plus the BotFather token and chat ID to receive notifications on each delegation and when failures occur. Alerts are retried gracefully so a transient network issue will not interrupt the main loop.

---

**Security note:** keep your keyring secure and consider running the bot on hardened infrastructure. Always test with a small wallet before committing large balances.
