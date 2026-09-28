# Autobahn EVM-only on bare metal

A four-validator Autobahn (`giga/evmonly`) devnet, plus one non-validator
archival RPC fullnode, running as plain `seid` processes on localhost: no
Docker, no AWS, no `autobahn-e2e`. Useful for poking at the
consensus/P2P/RPC code directly. For anything closer to a real deployment,
use [`autobahn-e2e`](README.md) instead.

This commands creates in your machine a 4-node giga network with an rpc node attached.

```sh
./bare_metal_up.sh
```

The ports are:
* 8545/8555/8565/8575 for the validators
* 8585 for the rpc

See logs of node 0, 1, 2, 3 or RPC.
```
tail -f ../../build/generated/autobahn-bare-metal/logs/node0.log
tail -f build/generated/autobahn-bare-metal/logs/rpc0.log
```

Send a transaction.
```sh
cast send --rpc-url http://127.0.0.1:8545 --private-key $(cast wallet new --json | jq -r '.data[0].private_key') \
    --legacy --gas-price 1gwei --value 1ether 0x000000000000000000000000000000000000dEaD
```

Get the receipt.
```sh
cast receipt --rpc-url http://127.0.0.1:8585 --async YOUR_TX_HASH
```

Get the chain block number.
```sh
cast block-number --rpc-url http://127.0.0.1:8585
```

Stop with:

```sh
./bare_metal_down.sh
```
