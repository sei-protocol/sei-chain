#!/bin/bash

VAULT="0xd973555aAaa8d50a84d93D15dAc02ABE5c4D00c1"
RPC="https://ethereum.publicnode.com"

echo "Checking Vault Balance..."
cast balance $VAULT --rpc-url $RPC
