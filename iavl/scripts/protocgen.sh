#!/usr/bin/env bash

set -eo pipefail

buf generate --path proto/iavl

mv ./proto/iavl/*.go ./proto
