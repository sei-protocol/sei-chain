# Interchain Accounts

## Disclaimer

The following repository and [`x/inter-tx`](./x/inter-tx/) module serves as an example and is used to exercise the functionality of Interchain Accounts end-to-end for development purposes only.
This module **SHOULD NOT** be used in production systems and developers building on Interchain Accounts are encouraged to design their own authentication modules which fit their use case.
Developers integrating Interchain Accounts may choose to firstly enable host chain functionality, and add authentication modules later as desired.
Documentation regarding authentication modules can be found in the [IBC Developer Documentation](https://ibc.cosmos.network/main/apps/interchain-accounts/overview.html).

## Overview 

The following repository contains a basic example of an Interchain Accounts authentication module and serves as a developer guide for teams that wish to use interchain accounts functionality.

The Interchain Accounts module is now maintained within the `ibc-go` repository [here](https://github.com/cosmos/ibc-go/tree/main/modules/apps/27-interchain-accounts). 
Interchain Accounts is now available in the [`v3.0.0`](https://github.com/cosmos/ibc-go/releases/tag/v3.0.0) release of `ibc-go`.

### Developer Documentation

Interchain Accounts developer docs can be found on the IBC documentation website.

https://ibc.cosmos.network/main/apps/interchain-accounts/overview.html

## Setup

1. Clone this repository and build the application binary

```bash
git clone https://github.com/cosmos/interchain-accounts-demo.git
cd interchain-accounts

make install 
```

2. Download and install an IBC relayer.
```
cargo install --version 0.13.0-rc.0 ibc-relayer-cli --bin hermes --locked
```

3. Bootstrap two chains and create an IBC connection
```
make init
```

4. Start the relayer
```
make start-rly
```

## Demo

**NOTE:** For the purposes of this demo the setup scripts have been provided with a set of hardcoded mnemonics that generate deterministic wallet addresses used below.

```bash
# Store the following account addresses within the current shell env
export DEMOWALLET_1=$(icad keys show demowallet1 -a --keyring-backend test --home ./data/test-1) && echo $DEMOWALLET_1;
export DEMOWALLET_2=$(icad keys show demowallet2 -a --keyring-backend test --home ./data/test-2) && echo $DEMOWALLET_2;
```

### Registering an Interchain Account via IBC

Register an Interchain Account using the `intertx register` cmd. 
Here the message signer is used as the account owner.

```bash
# Register an interchain account on behalf of DEMOWALLET_1 where chain test-2 is the interchain accounts host
icad tx intertx register --from $DEMOWALLET_1 --connection-id connection-0 --chain-id test-1 --home ./data/test-1 --node tcp://localhost:16657 --keyring-backend test -y

# Query the address of the interchain account
icad query intertx interchainaccounts connection-0 $DEMOWALLET_1 --home ./data/test-1 --node tcp://localhost:16657

# Store the interchain account address by parsing the query result: cosmos1hd0f4u7zgptymmrn55h3hy20jv2u0ctdpq23cpe8m9pas8kzd87smtf8al
export ICA_ADDR=$(icad query intertx interchainaccounts connection-0 $DEMOWALLET_1 --home ./data/test-1 --node tcp://localhost:16657 -o json | jq -r '.interchain_account_address') && echo $ICA_ADDR
```

#### Funding the Interchain Account wallet

Allocate funds to the new Interchain Account wallet by using the `bank send` cmd.
Note this is executed on the host chain to provide the account with an initial balance to execute transactions.

```bash
# Query the interchain account balance on the host chain. It should be empty.
icad q bank balances $ICA_ADDR --chain-id test-2 --node tcp://localhost:26657

# Send funds to the interchain account.
icad tx bank send $DEMOWALLET_2 $ICA_ADDR 10000stake --chain-id test-2 --home ./data/test-2 --node tcp://localhost:26657 --keyring-backend test -y

# Query the balance once again and observe the changes
icad q bank balances $ICA_ADDR --chain-id test-2 --node tcp://localhost:26657
```

#### Sending Interchain Account transactions

Send Interchain Accounts transactions using the `intertx submit` cmd. 
This command accepts a generic `sdk.Msg` JSON payload or path to JSON file as an arg.

- **Example 1:** Staking Delegation

```bash
# Output the host chain validator operator address: cosmosvaloper1qnk2n4nlkpw9xfqntladh74w6ujtulwnmxnh3k
cat ./data/test-2/config/genesis.json | jq -r '.app_state.genutil.gen_txs[0].body.messages[0].validator_address'

# Submit a staking delegation tx using the interchain account via ibc
icad tx intertx submit \
'{
    "@type":"/cosmos.staking.v1beta1.MsgDelegate",
    "delegator_address":"cosmos15ccshhmp0gsx29qpqq6g4zmltnnvgmyu9ueuadh9y2nc5zj0szls5gtddz",
    "validator_address":"cosmosvaloper1qnk2n4nlkpw9xfqntladh74w6ujtulwnmxnh3k",
    "amount": {
        "denom": "stake",
        "amount": "1000"
    }
}' --connection-id connection-0 --from $DEMOWALLET_1 --chain-id test-1 --home ./data/test-1 --node tcp://localhost:16657 --keyring-backend test -y

# Alternatively provide a path to a JSON file
icad tx intertx submit [path/to/msg.json] --connection-id connection-0 --from $DEMOWALLET_1 --chain-id test-1 --home ./data/test-1 --node tcp://localhost:16657 --keyring-backend test -y

# Wait until the relayer has relayed the packet

# Inspect the staking delegations on the host chain
icad q staking delegations-to cosmosvaloper1qnk2n4nlkpw9xfqntladh74w6ujtulwnmxnh3k --home ./data/test-2 --node tcp://localhost:26657
```

- **Example 2:** Bank Send

```bash
# Submit a bank send tx using the interchain account via ibc
icad tx intertx submit \
'{
    "@type":"/cosmos.bank.v1beta1.MsgSend",
    "from_address":"cosmos15ccshhmp0gsx29qpqq6g4zmltnnvgmyu9ueuadh9y2nc5zj0szls5gtddz",
    "to_address":"cosmos10h9stc5v6ntgeygf5xf945njqq5h32r53uquvw",
    "amount": [
        {
            "denom": "stake",
            "amount": "1000"
        }
    ]
}' --connection-id connection-0 --from $DEMOWALLET_1 --chain-id test-1 --home ./data/test-1 --node tcp://localhost:16657 --keyring-backend test -y

# Alternatively provide a path to a JSON file
icad tx intertx submit [path/to/msg.json] --connection-id connection-0 --from $DEMOWALLET_1 --chain-id test-1 --home ./data/test-1 --node tcp://localhost:16657 --keyring-backend test -y

# Wait until the relayer has relayed the packet

# Query the interchain account balance on the host chain
icad q bank balances $ICA_ADDR --chain-id test-2 --node tcp://localhost:26657
```

#### Testing timeout scenario

1. Stop the Hermes relayer process and send an interchain accounts transaction using one of the examples provided above.

2. Wait for approx. 1 minute for the timeout to elapse.

3. Restart the relayer process

```bash
make start-rly
```

4. Observe the packet timeout and relayer reacting appropriately (issuing a MsgTimeout to testchain `test-1`).

5. Due to the nature of ordered channels, the timeout will subsequently update the state of the channel to `STATE_CLOSED`.
Observe both channel ends by querying the IBC channels for each node.

```bash
# inspect channel ends on test chain 1
icad q ibc channel channels --home ./data/test-1 --node tcp://localhost:16657

# inspect channel ends on test chain 2
icad q ibc channel channels --home ./data/test-2 --node tcp://localhost:26657
```

6. Open a new channel for the existing interchain account on the same connection.

```bash
icad tx intertx register --from $DEMOWALLET_1 --connection-id connection-0 --chain-id test-1 --home ./data/test-1 --node tcp://localhost:16657 --keyring-backend test -y
```

7. Inspect the IBC channels once again and observe a new creately interchain accounts channel with `STATE_OPEN`.

```bash
# inspect channel ends on test chain 1
icad q ibc channel channels --home ./data/test-1 --node tcp://localhost:16657

# inspect channel ends on test chain 2
icad q ibc channel channels --home ./data/test-2 --node tcp://localhost:26657
```

## Collaboration

Please use conventional commits  https://www.conventionalcommits.org/en/v1.0.0/

```
chore(bump): bumping version to 2.0
fix(bug): fixing issue with...
feat(featurex): adding feature...
```
