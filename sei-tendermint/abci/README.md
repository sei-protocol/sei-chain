# Application BlockChain Interface (ABCI)

Blockchains are systems for multi-master state machine replication.
**ABCI** is an interface that defines the boundary between the replication engine (the blockchain),
and the state machine (the application).
Using a socket protocol, a consensus engine running in one process
can manage an application state running in another.

Previously, the ABCI was referred to as TMSP.

The community has provided a number of additional implementations, see the [Tendermint Ecosystem](https://github.com/tendermint/awesome#ecosystem)


## Installation & Usage

See the [examples](./example/) directory for sample ABCI applications.

## Specification

A detailed description of the ABCI methods and message types is contained in:

- [A protobuf file](../proto/tendermint/abci/types.proto)
- [A Go interface](./types/application.go)

## Protocol Buffers

To compile the protobuf file, run (from the root of the repo):

```sh
./scripts/protoc.sh
```

See `protoc --help` and [the Protocol Buffers site](https://developers.google.com/protocol-buffers)
for details on compiling for other languages. Note we also include a [GRPC](https://www.grpc.io/docs)
service definition.
