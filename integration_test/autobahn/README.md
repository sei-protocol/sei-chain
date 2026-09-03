# Autobahn EVM-only E2E clusters

`autobahn-e2e` manages the four-validator, in-memory EVM-only Autobahn
topology used by the integration load test. It keeps cluster metadata under
`~/.sei/autobahn-e2e` by default. Override that location with
`--state-dir` or `AUTOBAHN_E2E_STATE_DIR`.

Build the command once:

```sh
make build-autobahn-e2e
```

This creates `./autobahn-e2e` outside `build/` so the Docker node build's
`make clean` does not remove the cluster manager while deploying.

## Local Docker

Deploy the cluster from the current checkout, inspect every node, and expose a
chosen node on another local port:

```sh
./autobahn-e2e deploy --target local
./autobahn-e2e list
./autobahn-e2e forward --node node-2 --local-port 18545
./autobahn-e2e teardown
```

The existing Compose topology publishes each container's port 8545 on a unique
host port. `list` shows those ports. `forward` starts a TCP relay when the
requested port differs from the existing mapping.

The EVM-only network does not start Tendermint RPC, the Cosmos REST API, or
gRPC. Port 8545 exposes a deliberately minimal JSON-RPC service with only
`eth_sendRawTransaction`; other EVM methods currently return method-not-found.
`list` reads execution height from the node's internal Prometheus endpoint
inside its container, so cluster inspection does not require Tendermint RPC.

## AWS EC2

The AWS target provisions one Ubuntu EC2 host and runs the identical four-node
Docker topology on it. Only SSH is opened in the managed security group; EVM
JSON-RPC remains private and is reached with `forward`.

```sh
./autobahn-e2e deploy --target aws \
  --name my-autobahn \
  --region us-west-2

./autobahn-e2e list --name my-autobahn
./autobahn-e2e forward \
  --name my-autobahn \
  --node node-1 \
  --local-port 18545
./autobahn-e2e teardown --name my-autobahn
```

AWS credentials are resolved through the AWS CLI credential chain. When no
credentials work and the command is attached to a terminal, it starts
`aws configure` interactively. Credentials are never copied into cluster state.

By default, deployment creates an EC2 key pair and stores its private key next
to the cluster state with mode `0600`. Teardown deletes both. To use an existing
key pair instead:

```sh
./autobahn-e2e deploy --target aws \
  --key-name my-key-pair \
  --ssh-key ~/.ssh/my-key-pair.pem
```

The default security-group rule allows SSH only from the public IP detected at
deployment time. Use `--ssh-cidr` when running behind a VPN, through NAT with a
different egress address, or from an IPv6 network.

The default EC2 shape is `c7g.2xlarge` with the current Ubuntu 24.04 ARM64 AMI
resolved from AWS Systems Manager. Override `--instance-type` and `--ami-id`
together when using another architecture. `--repo-url` and `--ref` select the
source deployed remotely; they default to the current checkout's origin and
commit.

The in-memory topology can be tuned through environment variables passed to
the cluster start target:

```sh
AUTOBAHN=true \
AUTOBAHN_EVMONLY_IN_MEMORY=true \
AUTOBAHN_EVMONLY_MAX_TXS_PER_BLOCK=5000 \
AUTOBAHN_EVMONLY_BLOCK_INTERVAL=100ms \
AUTOBAHN_EVMONLY_MAX_GAS=120000000 \
AUTOBAHN_EVMONLY_MEMPOOL_SIZE=100000 \
AUTOBAHN_EVMONLY_GOGC=200 \
AUTOBAHN_EVMONLY_GOMAXPROCS=16 \
AUTOBAHN_EVMONLY_ENABLE_EVM_PROXY=false \
DOCKER_DETACH=true \
make docker-cluster-start-skipbuild
```

The defaults remain 2,000 transactions per block, a 400 ms block interval,
35,000,000 gas, and 5,000 mempool entries. `GOGC` and `GOMAXPROCS` retain the
Go runtime defaults unless set explicitly. EVM RPC proxying defaults to true;
disable it only for load tests that can submit independent sender streams to
every validator.

If provisioning fails after AWS resources are created, the state is retained
with status `failed`. Run `list` to inspect it and `teardown` to remove the
instance, security group, and any managed key pair.
