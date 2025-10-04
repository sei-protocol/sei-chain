# Termux support for the Solo claim transaction builder

These helper scripts make it easier to use `tools/solo/build_claim_tx.py` from a
[Termux](https://termux.dev/) environment on Android devices.

## Installation

Run the dependency installer once to ensure all required system packages and
Python libraries are available:

```sh
bash tools/solo/termux/install_dependencies.sh
```

The script performs the following steps:

1. Updates and upgrades the Termux package repositories.
2. Installs Python and the native toolchain that `web3` and `eth-account`
   depend on (Rust, clang, binutils, libffi, OpenSSL).
3. Installs the Python dependencies listed in
   [`requirements.txt`](./requirements.txt).

> **Tip:** If you are running Termux on an older device, you may need to run the
> command twice when Termux prompts for repository key upgrades.

## Running the builder

After the dependencies are installed, invoke the wrapper script to execute the
builder with the same arguments you would pass on desktop platforms:

```sh
bash tools/solo/termux/run_claim_builder.sh \
  --payload /sdcard/payload.hex \
  --chain-id 1329 \
  --rpc-url "https://sei-evm.example.org" \
  --private-key "$PRIVATE_KEY"
```

The wrapper simply forwards all parameters to the Python entry point using the
Termux `python3` binary. You can override the Python interpreter by setting the
`PYTHON_BIN` environment variable.

## Updating dependencies

If the Python libraries are updated upstream, rerun the installer script to
upgrade the packages:

```sh
bash tools/solo/termux/install_dependencies.sh
```

This will upgrade both the Termux packages and the Python dependencies to the
latest compatible versions.
