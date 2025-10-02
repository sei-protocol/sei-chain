#!/bin/bash
#
# Update the generated code for protocol buffers in the Tendermint repository.
# This must be run from inside a Tendermint working directory.
#
set -euo pipefail

# Work from the root of the repository.
cd "$(git rev-parse --show-toplevel)"

make proto-gen
