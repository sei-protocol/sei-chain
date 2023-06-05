# Token Factory

The tokenfactory module allows any account to create a new token with
the name `factory/{creator address}/{subdenom}`. Because tokens are
namespaced by creator address, this allows token minting to be
permissionless, due to not needing to resolve name collisions. A single
account can create multiple denoms, by providing a unique subdenom for each
created denom. Once a denom is created, the original creator is given
"admin" privileges over the asset. This allows them to:

- Mint their denom to any account
- Burn their denom from any account
- Create a transfer of their denom between any two accounts
- Change the admin. In the future, more admin capabilities may be added. Admins
  can choose to share admin privileges with other accounts using the authz
  module. The `ChangeAdmin` functionality, allows changing the master admin
  account, or even setting it to `""`, meaning no account has admin privileges
  of the asset.

## Messages

### CreateDenom

Creates a denom of `factory/{creator address}/{subdenom}` given the denom creator
address and the subdenom. Subdenoms can contain `[a-zA-Z0-9./]`.

```go
message MsgCreateDenom {
  string sender = 1 [ (gogoproto.moretags) = "yaml:\"sender\"" ];
  string subdenom = 2 [ (gogoproto.moretags) = "yaml:\"subdenom\"" ];
}
```

**State Modifications:**

- Set `DenomMetaData` via bank keeper.
- Set `AuthorityMetadata` for the given denom to store the admin for the created
  denom `factory/{creator address}/{subdenom}`. Admin is automatically set as the
  Msg sender.
- Add denom to the `CreatorPrefixStore`, where a state of denoms created per
  creator is kept.

### Mint

Minting of a specific denom is only allowed for the current admin.
Note, the current admin is defaulted to the creator of the denom.

```protobuf
message MsgMint {
  string sender = 1 [ (gogoproto.moretags) = "yaml:\"sender\"" ];
  cosmos.base.v1beta1.Coin amount = 2 [
    (gogoproto.moretags) = "yaml:\"amount\"",
    (gogoproto.nullable) = false
  ];
}
```

**State Modifications:**

- Safety check the following
  - Check that the denom minting is created via `tokenfactory` module
  - Check that the sender of the message is the admin of the denom
- Mint designated amount of tokens for the denom via `bank` module

### Burn

Burning of a specific denom is only allowed for the current admin.
Note, the current admin is defaulted to the creator of the denom.

```protobuf
message MsgBurn {
  string sender = 1 [ (gogoproto.moretags) = "yaml:\"sender\"" ];
  cosmos.base.v1beta1.Coin amount = 2 [
    (gogoproto.moretags) = "yaml:\"amount\"",
    (gogoproto.nullable) = false
  ];
}
```

**State Modifications:**

- Safety check the following
  - Check that the denom is created via `tokenfactory` module
  - Check that the sender of the message is the admin of the denom
- Burn designated amount of tokens for the denom via `bank` module

### ChangeAdmin

Change the admin of a denom. Note, this is only allowed to be called by the current admin of the denom.

```protobuf
message MsgChangeAdmin {
  string sender = 1 [ (gogoproto.moretags) = "yaml:\"sender\"" ];
  string denom = 2 [ (gogoproto.moretags) = "yaml:\"denom\"" ];
  string newAdmin = 3 [ (gogoproto.moretags) = "yaml:\"new_admin\"" ];
}
```

**State Modifications:**

- Check that sender of the message is the admin of denom
- Modify `AuthorityMetadata` state entry to change the admin of the denom
