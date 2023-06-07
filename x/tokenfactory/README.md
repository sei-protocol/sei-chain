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

## Tokenfactory Denom Restrictions

Tokenfactory denoms are of form `factory/{creator address}/{subdenom}`.

- The Max Subdenom length is 44 characters
- The Max Creator length is 75 characters
- All tokenfactory denoms will begin with `factory`

Please reference the Appendix for more details on the derivation of these limits.

# Examples
To create a new token, use the create-denom command from the tokenfactory module. The following example uses the address sei166vhptur29s3gw5qr6dm30s06gej6pr4n6qc4l from mylocalwallet as the default admin for the new token.

## Creating a token
To create a new token we can use the create-denom command.

```sh
seid tx tokenfactory create-denom ufoo --from mylocalwallet
```

## Mint a new token
Once a new token is created, it can be minted using the mint command in the tokenfactory module. Note that the complete tokenfactory address, in the format of factory/{creator address}/{subdenom}, must be used to mint the token.

```sh
seid tx tokenfactory mint 100000000000factory/sei166vhptur29s3gw5qr6dm30s06gej6pr4n6qc4l/ufoo --from mylocalwallet
```

## Checking Token metadata
To view a token's metadata, use the denom-metadata command in the bank module. The following example queries the metadata for the token factory/sei166vhptur29s3gw5qr6dm30s06gej6pr4n6qc4l/ufoo:

```sh
seid query bank denom-metadata --denom factory/sei166vhptur29s3gw5qr6dm30s06gej6pr4n6qc4l/ufoo
```

## Check the tokens created by an account
To see a list of tokens created by a specific account, use the denoms-from-creator command in the tokenfactory module. The following example shows tokens created by the account sei166vhptur29s3gw5qr6dm30s06gej6pr4n6qc4l:

```sh
seid query tokenfactory denoms-from-creator sei166vhptur29s3gw5qr6dm30s06gej6pr4n6qc4ls
```

## Appendix: Expectations from the Chain

As mentioned above, the chain's bech32 prefix for addresses can be at most 16 characters long.

This comes from denoms having a 128 byte maximum length, enforced from the SDK,
and us setting longest_subdenom to be 44 bytes.

A token factory token's denom is: `factory/{creator address}/{subdenom}`

Splitting up into sub-components, this has:

- `len(factory) = 7`
- `2 * len("/") = 2`
- `len(longest_subdenom)`
- `len(creator_address) = len(bech32(longest_addr_length, chain_addr_prefix))`.

Longest addr length at the moment is `32 bytes`. Due to SDK error correction
settings, this means `len(bech32(32, chain_addr_prefix)) = len(chain_addr_prefix) + 1 + 58`.
Adding this all, we have a total length constraint of `128 = 7 + 2 + len(longest_subdenom) + len(longest_chain_addr_prefix) + 1 + 58`.
Therefore `len(longest_subdenom) + len(longest_chain_addr_prefix) = 128 - (7 + 2 + 1 + 58) = 60`.

Considerations for how we split these 60 bytes between maxes from
longest_subdenom and longest_chain_addr_prefix:

- Per [BIP-0173](https://github.com/bitcoin/bips/blob/master/bip-0173.mediawiki#bech32)
  the technically longest HRP for a 32 byte address ('data field') is 31 bytes.
  (Comes from encode(data) = 59 bytes, and max length = 90 bytes)
- subdenom should be at least 32 bytes so hashes can go into it
- longer subdenoms are very helpful for creating human readable denoms
- chain addresses should prefer being smaller. The longest HRP in cosmos to date is 11 bytes. (`persistence`)

For explicitness, its currently set to `len(longest_subdenom) = 44` and `len(longest_chain_addr_prefix) = 16`.

Please note, if the SDK increases the maximum length of a denom from 128 bytes,
these caps should increase.

So please don't make code rely on these max lengths for parsing.
