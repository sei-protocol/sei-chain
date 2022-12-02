#!/usr/bin/env sh

# Input parameters
NODE_ID=${ID:-0}

# Executing
echo "hello-sei"
cd /sei-chain
ls -l

# Testing whether seid works or not
./seid version

# TODO: Add sei-chain initialization logic