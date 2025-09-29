# Holo Protocol Integration

This package ships a lightweight implementation of the "Holo Protocol" demo referenced in the
`Pray4Love1/holo-protocol-internal` repository.  The goal is to keep the code available inside the
`sei-x402` monorepo as a stand-alone example without pulling in the entire private repository.

The mini implementation captures the core ideas of the original project:

* **SoulKey identity** – simple local identity bootstrap stored under `~/.holo_protocol/profile.json`.
* **Time-based one-time password (TOTP)** – provisioning URI and ASCII QR rendering so an authenticator
  app can be linked to the generated SoulKey.
* **Real-time Sei alerts** – a small simulator that streams sample transfer events to demonstrate how a
  CLI can react to activity on the Sei network.
* **Guardian checks** – a deterministic risk engine that evaluates transfers and raises warnings for
  anomalous behaviour.

## Installation

Create a virtual environment (or use an existing tooling solution) and install the package in editable
mode so the CLI can be invoked locally:

```bash
uv pip install -e ./python/holo_protocol
```

This exposes the `holo-cli` command which provides the same entry points as the reference
implementation.

## Usage

```bash
# Initialise a new SoulKey profile and generate a TOTP secret
holo-cli setup --address sei1example... --label "alice@holo"

# Show account status, including the provisioning URI and optional ASCII QR code
holo-cli status --show-qr

# Tail sample Sei alerts
holo-cli alerts --address sei1example... --limit 5

# Evaluate a transaction payload with the Guardian heuristics
holo-cli guardian --tx-file path/to/payload.json
```

A sample payload can be found in `src/holo_protocol/data/sample_transaction.json` and the alerts
simulator consumes `src/holo_protocol/data/sample_alerts.json`.

## Testing

The module ships with a focused test-suite:

```bash
cd python/holo_protocol
uv run pytest
```

This ensures the SoulKey provisioning, TOTP calculations, and Guardian risk checks behave deterministically.
