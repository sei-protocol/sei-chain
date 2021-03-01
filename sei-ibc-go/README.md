# ibc-go
<div align="center">
  <a href="https://github.com/cosmos/ibc-go/releases/latest">
    <img alt="Version" src="https://img.shields.io/github/tag/cosmos/ibc-go.svg" />
  </a>
  <a href="https://github.com/cosmos/ibc-go/blob/main/LICENSE">
    <img alt="License: Apache-2.0" src="https://img.shields.io/github/license/cosmos/ibc-go.svg" />
  </a>
  <a href="https://pkg.go.dev/github.com/cosmos/ibc-go?tab=doc">
    <img alt="GoDoc" src="https://godoc.org/github.com/cosmos/ibc-go?status.svg" />
  </a>
  <a href="https://goreportcard.com/report/github.com/cosmos/ibc-go">
    <img alt="Go report card" src="https://goreportcard.com/badge/github.com/cosmos/ibc-go" />
  </a>
  <a href="https://codecov.io/gh/cosmos/ibc-go">
    <img alt="Code Coverage" src="https://codecov.io/gh/cosmos/ibc-go/branch/main/graph/badge.svg" />
  </a>
</div>
<div align="center">
  <a href="https://github.com/cosmos/ibc-go">
    <img alt="Lines Of Code" src="https://tokei.rs/b1/github/cosmos/ibc-go" />
  </a>
  <a href="https://discord.gg/AzefAFd">
    <img alt="Discord" src="https://img.shields.io/discord/669268347736686612.svg" />
  </a>
  <a href="https://sourcegraph.com/github.com/cosmos/ibc-go?badge">
    <img alt="Imported by" src="https://sourcegraph.com/github.com/cosmos/ibc-go/-/badge.svg" />
  </a>
    <img alt="Lint Status" src="https://github.com/cosmos/cosmos-sdk/workflows/Lint/badge.svg" />
</div>
Interblockchain communication protocol (IBC) implementation in Golang built as a SDK module. 

## Components

### Core

The `core/` directory contains the SDK IBC module that SDK based chains must integrate in order to utilize this implementation of IBC.
It handles the core components of IBC including clients, connection, channels, packets, acknowledgements, and timeouts. 

### Applications

Applications can be built as modules to utilize core IBC by fulfilling a set of callbacks. 
Fungible Token Transfers is currently the only supported application module. 

### IBC Light Clients

IBC light clients are on-chain implementations of an off-chain light clients.
This repository currently supports tendermint and solo-machine light clients. 
The localhost client is currently non-functional. 

## Docs

Please see our [documentation](docs/README.md) for more information.

