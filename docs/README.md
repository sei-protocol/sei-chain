# sei-chain × zk402 Protocol

**Sovereign blockchain infrastructure with integrated zk402 universal payments**

## 🏗️ Repository Structure
```
sei-chain/
├── index.html              # Public-facing status page (Vercel)
├── cmd/                    # Sei chain Go binaries
├── x/                      # Cosmos SDK modules
├── app/                    # Chain application code
├── docs/                   # Chain documentation
└── README.md              # This file
```

## 🌐 Deployments

### Frontend (Vercel)
- **Production:** https://zk402.vercel.app
- **Purpose:** Public status surface for zk402 protocol
- **Technology:** Static HTML (no server required)

### Chain Infrastructure (Dedicated Nodes)
- **Validators:** Run on bare metal/VMs
- **RPC Nodes:** Separate infrastructure
- **Not deployed on Vercel**

## 🚀 Quick Start

### View Status Page
Simply visit the Vercel deployment URL. No local setup required.

### Run Sei Chain Locally
```bash
# Install Go 1.21+
go version

# Build chain binary
make install

# Initialize node
seid init my-node --chain-id sei-testnet-1

# Start node
seid start
```

## 📋 Development Workflow

### Frontend Changes
1. Edit `index.html`
2. Commit and push to main branch
3. Vercel auto-deploys (< 30 seconds)

### Chain Changes
1. Modify Go code in `cmd/`, `x/`, or `app/`
2. Rebuild: `make install`
3. Test locally before deploying to validators

## 🔐 zk402 Protocol Integration

This repository includes the **x402 universal payment protocol**:
- Sovereign wallet generation
- Cross-chain settlement
- Code attribution tracking
- Entropy-sealed transactions (ψ = 3.12)

See `docs/x402-protocol.md` for implementation details.

## ⚠️ Important Notes

- **Vercel deploys ONLY the frontend** (`index.html`)
- **Never commit private keys or sensitive data**
- **Chain validators run on dedicated infrastructure**
- **RPC endpoints are queried client-side via JavaScript**

## 📖 Documentation

- [Sei Chain Docs](https://docs.sei.io)
- [x402 Protocol Spec](docs/x402-protocol.md)
- [Deployment Guide](docs/deployment.md)

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch
3. Make changes
4. Submit a pull request

## 📄 License

See LICENSE file for details.

## 🔗 Links

- **GitHub:** https://github.com/Pray4Love1/sei-chain
- **Status Page:** https://zk402.vercel.app
- **Sei Network:** https://sei.io

---

Built with ψ = 3.12 | The Light is Yours
# OpenAPI/Swagger docs generation

> **Note:** Anytime we make changes to the APIs/proto files, we also need to update the Swagger/OpenAPI docs.

Both Swagger and OpenAPI terms are used interchangeably in this document.
-  [OpenAPI](https://github.com/OAI/OpenAPI-Specification/) = specification
- Swagger = Tools for implementing the specification.

The process of Swagger docs generation involves running a script that does the following tasks:
1. Generating the swagger yml file using the `ignite` tool.
2. Updating the static assets for the Swagger UI.

## Prerequisites

The docs generation script uses the `ignite` tool to generate the OpenAPI docs.
So, first install the `ignite` tool, if not installed already.
We need version v0.23.0, which is outdated, but works with the current version of the codebase.
Pull binaries from the [releases page](https://github.com/ignite/cli/releases/tag/v0.23.0) or install from source code 
following instructions.

Verify the installation by running `ignite version`:

```bash
% ignite version          
·
· 🛸 Ignite CLI v28.2.0 is available!
·
· To upgrade your Ignite CLI version, see the upgrade doc: https://docs.ignite.com/guide/install.html#upgrading-your-ignite-cli-installation
·
··

Ignite CLI version:     v0.23.0
....

```
## Running the script
Then run the following command to generate the OpenAPI docs and update static assets for Swagger UI:

```bash
./scripts/update-swagger-ui-statik.sh
```
The script generates a new swagger/openapi yml and saves it as `docs/swagger-ui/swagger.yml`.
It will then embed the yml file and static assets in `docs/swagger-ui/` directory into the `docs/swagger/statik.go` file.

If swagger endpoint is configured, the app on startup will "read" `statik.go` and make it's content available at the 
`/swagger/` endpoint.

# Serving the OpenAPI docs on the node

To start serving the docs, enable the swagger docs serving. 
To do that, update `api.swagger` value to `true` in the `app.toml` file.

```toml
###############################################################################
###                           API Configuration                             ###
###############################################################################

[api]

# Enable defines if the API server should be enabled.
enable = true

# Swagger defines if swagger documentation should automatically be registered.
swagger = true
```
Once node is restarted, swagger docs will be available at `http://<node-ip>:<port>/swagger/`
