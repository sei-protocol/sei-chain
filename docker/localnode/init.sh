#!/usr/bin/env sh

# Input parameters
NODE_ID=${ID:-0}

# Greeting output
echo "hello-sei"

# Uncomment the below line if there are any dependency issues
# ldd build/seid

# Testing whether seid works or not
pwd && ls -l
./build/seid version

# TODO: Add sei-chain initialization logic