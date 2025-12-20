# Deployment Guide

## Frontend Deployment (Vercel)

### Initial Setup

1. **Connect GitHub Repository**
   - Go to [vercel.com](https://vercel.com)
   - Click "New Project"
   - Import `Pray4Love1/sei-chain`
   - Select main branch

2. **Configure Project**
   - **Framework Preset:** None (Static)
   - **Root Directory:** `/` (leave empty)
   - **Build Command:** (leave empty)
   - **Output Directory:** `.` (leave empty)

3. **Deploy**
   - Click "Deploy"
   - Wait ~30 seconds
   - Your site is live!

### Environment Variables

**No environment variables required** for the static frontend.

If you add API endpoints later, configure:
```
NEXT_PUBLIC_RPC_URL=https://rpc.sei-apis.com
```

### Auto-Deployment

Every commit to `main` branch triggers automatic deployment:
1. Push changes: `git push origin main`
2. Vercel detects changes
3. Builds and deploys (< 30 seconds)
4. Updates production URL

### Custom Domain

1. Go to Vercel project settings
2. Click "Domains"
3. Add your domain (e.g., `zk402.io`)
4. Update DNS records as instructed
5. SSL certificate auto-generated

---

## Chain Deployment (Validators)

### Prerequisites

- Ubuntu 22.04 LTS or similar
- 4+ CPU cores
- 16GB+ RAM
- 500GB+ SSD storage
- Go 1.21+

### Installation
```bash
# Clone repository
git clone https://github.com/Pray4Love1/sei-chain.git
cd sei-chain

# Install dependencies
make install

# Verify installation
seid version
```

### Initialize Node
```bash
# Initialize chain
seid init my-validator --chain-id sei-testnet-1

# Download genesis file
wget https://raw.githubusercontent.com/sei-protocol/testnet/main/sei-testnet-1/genesis.json -O ~/.sei/config/genesis.json

# Add seed nodes
vim ~/.sei/config/config.toml
# Update seeds = "..."
```

### Start Validator
```bash
# Create systemd service
sudo tee /etc/systemd/system/seid.service > /dev/null <<EOF
[Unit]
Description=Sei Node
After=network-online.target

[Service]
User=$USER
ExecStart=$(which seid) start
Restart=on-failure
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

# Enable and start
sudo systemctl enable seid
sudo systemctl start seid

# Check logs
journalctl -u seid -f
```

### Create Validator
```bash
# Create validator key
seid keys add validator

# Fund validator account (testnet faucet)
# Visit: https://faucet.sei.io

# Create validator
seid tx staking create-validator \
  --amount=1000000usei \
  --pubkey=$(seid tendermint show-validator) \
  --moniker="My Validator" \
  --chain-id=sei-testnet-1 \
  --commission-rate="0.10" \
  --commission-max-rate="0.20" \
  --commission-max-change-rate="0.01" \
  --min-self-delegation="1" \
  --gas="auto" \
  --gas-adjustment=1.5 \
  --from=validator
```

---

## RPC Node Deployment

### Configuration
```bash
# Enable RPC
vim ~/.sei/config/config.toml

# Update:
[rpc]
laddr = "tcp://0.0.0.0:26657"
cors_allowed_origins = ["*"]
```

### Nginx Reverse Proxy
```nginx
server {
    listen 80;
    server_name rpc.yourdomain.com;

    location / {
        proxy_pass http://localhost:26657;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
    }
}
```

### SSL with Certbot
```bash
sudo apt install certbot python3-certbot-nginx
sudo certbot --nginx -d rpc.yourdomain.com
```

---

## Monitoring

### Prometheus Setup
```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'sei'
    static_configs:
      - targets: ['localhost:26660']
```

### Grafana Dashboard

1. Install Grafana
2. Add Prometheus data source
3. Import Cosmos dashboard (ID: 11036)

---

## Security Best Practices

### Firewall Configuration
```bash
# Allow SSH
sudo ufw allow 22/tcp

# Allow P2P (Tendermint)
sudo ufw allow 26656/tcp

# Allow RPC (if public)
sudo ufw allow 26657/tcp

# Enable firewall
sudo ufw enable
```

### Key Management

- **Never commit private keys to Git**
- Store validator keys in encrypted storage
- Use hardware security modules (HSM) for mainnet
- Regular backups of `~/.sei/config/priv_validator_key.json`

### Updates
```bash
# Stop node
sudo systemctl stop seid

# Pull latest code
cd sei-chain
git pull origin main

# Rebuild
make install

# Restart
sudo systemctl start seid
```

---

## Troubleshooting

### Node Not Syncing
```bash
# Check peers
seid status | jq .SyncInfo

# Add more peers
vim ~/.sei/config/config.toml
# Update persistent_peers
```

### High Memory Usage
```bash
# Prune old blocks
vim ~/.sei/config/app.toml

[pruning]
pruning = "custom"
pruning-keep-recent = "100"
pruning-keep-every = "0"
pruning-interval = "10"
```

### RPC Timeout
```bash
# Increase timeout
vim ~/.sei/config/config.toml

[rpc]
timeout_broadcast_tx_commit = "30s"
```

---

## Support

- **GitHub Issues:** https://github.com/Pray4Love1/sei-chain/issues
- **Discord:** (add your server link)
- **Email:** support@yourdomain.com

---

ψ = 3.12 | The Light is Yours
