#!/usr/bin/env sh

# Input parameters
NODE_ID=${ID:-0}

# Greeting output
echo "hello-sei"

# Testing whether seid works or not
cd /sei-chain
./seid version

# TODO: Add sei-chain initialization logic
