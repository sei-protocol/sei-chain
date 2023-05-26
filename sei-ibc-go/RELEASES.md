# Releases

IBC-Go follows [semantic versioning](https://semver.org), but with the following deviations:

- A state-machine breaking change will result in an increase of the minor version Y (x.Y.z | x > 0).
- An API breaking change will result in an increase of the major number (X.y.z | x > 0). Please note that these changes **will be backwards compatible** (as opposed to canonical semantic versioning; read [Backwards compatibility](#backwards) for a detailed explanation).

This is visually explained in the following decision tree:

<p align="center">
  <img src="releases-decision-tree.png?raw=true" alt="Releases decision tree" width="40%" />
</p>

When bumping the dependencies of [Cosmos SDK](https://github.com/cosmos/cosmos-sdk) and [Tendermint](https://github.com/tendermint/tendermint) we will only treat patch releases as non state-machine breaking.

## <a name="backwards"></a> Backwards compatibility

[ibc-go](https://github.com/cosmos/ibc-go) and the [IBC protocol specification](https://github.com/cosmos/ibc) maintain different versions. Furthermore, ibc-go serves several different user groups (chains, IBC app developers, relayers, IBC light client developers). Each of these groups has different expectations of what *backwards compatible* means. It simply isn't possible to categorize a change as backwards or non backwards compatible for all user groups. We are primarily interested in when our API breaks and when changes are state machine breaking (thus requiring a coordinated upgrade). This is scoping the meaning of ibc-go to that of those interacting with the code (IBC app developers, relayers, IBC light client developers), not chains using IBC to communicate (that should be encapsulated by the IBC protocol specification versioning).

To summarize: **All our ibc-go releases allow chains to communicate successfully with any chain running any version of our code**. That is to say, we are still using IBC protocol specification v1.0. 

We ensure all major releases are supported by relayers ([hermes](https://github.com/informalsystems/ibc-rs), [rly](https://github.com/strangelove-ventures/relayer) and [ts-relayer](https://github.com/confio/ts-relayer) at the moment) which can relay between the new major release and older releases. We have no plans of upgrading to an IBC protocol specification v2.0, as this would be very disruptive to the ecosystem.

## Graphics

The decision tree above was generated with the following code:

```
%%{init: 
    {'theme': 'default',
     'themeVariables': 
        {'fontFamily': 'verdana', 'fontSize': '13px'}
    }
}%%
flowchart TD
    A(Change):::c --> B{API breaking?}
    B:::c --> |Yes| C(Increase major version):::c
    B:::c --> |No| D{state-machine breaking?}
    D:::c --> |Yes| G(Increase minor version):::c
    D:::c --> |No| H(Increase patch version):::c
    classDef c fill:#eee,stroke:#aaa
```

using [Mermaid](https://mermaid-js.github.io)'s [live editor](https://mermaid.live).
