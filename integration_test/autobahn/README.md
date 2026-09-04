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

The AWS target provisions four Ubuntu EC2 instances, one per validator. Each
instance builds the selected revision and runs `seid` directly as a systemd
service. Docker is not installed or used on the validator hosts. Builds run in
parallel, then node 0 generates the shared genesis and Autobahn configuration
that the command distributes to all four hosts.

Only SSH is exposed from the managed security group to the configured caller
CIDR. The instances may communicate with each other inside the security group;
EVM JSON-RPC remains private and is reached with `forward`.

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
resolved from AWS Systems Manager. For the throughput-test topology, select an
AMD64 compute instance explicitly and cap the Go scheduler at the measured
knee:

```sh
./autobahn-e2e deploy --target aws \
  --name throughput \
  --region us-east-2 \
  --architecture amd64 \
  --instance-type c8i.48xlarge \
  --gomaxprocs 24 \
  --gogc 200
```

Use `--ami-id` to override the Ubuntu image selected for `--architecture`.
`--repo-url` and `--ref` select the source deployed remotely; they default to
the current checkout's origin and commit. `--gomaxprocs 0` uses every logical
CPU on each instance. `--gogc off` is accepted for short ceiling tests but can
consume hundreds of GiB under sustained native-transfer load.

If provisioning fails after AWS resources are created, the state is retained
with status `failed`. Run `list` to inspect it and `teardown` to remove the
instances, security group, and any managed key pair.
