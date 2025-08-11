# Contributing

Thank you for considering making contributions to IAVL+! 
This repository follows the [contribution guidelines] of tendermint and the corresponding [coding repo]. 
Please take a look if you are not already familiar with those.

[contribution guidelines]: https://github.com/tendermint/tendermint/blob/master/CONTRIBUTING.md
[coding repo]: https://github.com/tendermint/coding

## Protobuf 

Iavl utilizes [Protocol Buffers](https://developers.google.com/protocol-buffers) if used as a gRPC server. To generate the protobuf stubs have docker running locally and run `make proto-gen`
