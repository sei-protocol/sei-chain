# x/epoch

The `x/epoch` module is engineered to manage epochs within the sei-chain ecosystem. An epoch is defined as a fixed period of time, defaulting to one minute, relative to the Genesis time. At the commencement of each epoch, registered actions by other modules are triggered.

This functionality enables time-centric actions and state transitions to be orchestrated throughout the sei-chain. Other modules can effortlessly register hooks via a simplistic interface provided by x/epoch, which are then executed at the onset of each epoch. This allows modules to carry out actions such as validator set updates, reward distributions, or parameter adjustments based on the progression of time.

**Example usage:**
The Mint module's end blocker employs the epoch hook to distribute inflation rewards to validators on specified dates.

## State

The x/epoch module upholds the following state:

```bash
> seid q epoch epoch --output json
{
  "epoch": {
    "genesis_time": "2023-04-27T19:08:11.958027Z",
    "epoch_duration": "60s",
    "current_epoch": "0",
    "current_epoch_start_time": "2023-04-27T19:08:11.958027Z",
    "current_epoch_height": "0"
  }
}
```

GenesisTime: The sei-chain's Genesis time.
EpochDuration: Duration of an epoch, denoted in seconds.
CurrentEpoch: Current epoch number.
EpochStartTime: Current epoch's start time.
CurrentEpochHeight: Height at which the current epoch was initiated.

## Messages

The `x/epoch` module does not extend any messages. All interactions with this module are carried out via hooks and events.

## Hooks

The `x/epoch` module exposes a set of hooks for other modules to implement. These hooks are called at the start and end of each epoch when BeginBlock verifies if it's the start or end of a given epoch.

**BeforeEpochStart**: This hook is called at the start of each epoch. Modules can leverage this hook to perform actions at the epoch's beginning.

```go
func (k Keeper) BeforeEpochStart(ctx sdk.Context, epoch epochTypes.Epoch) {
  ...
}
```

**AfterEpochEnd**: This hook is triggered at the end of each epoch. Modules can utilize this hook to execute actions at the epoch's conclusion.

```go
func (k Keeper) AfterEpochEnd(ctx sdk.Context, epoch epochTypes.Epoch) {
  ...
}
```

For an example of implementing these hooks, refer to `x/mint/keeper`. Hooks registration is completed in `app/app.go`:

```go
// New returns a reference to an initialized blockchain app
func New(...) {
  ...
  app.EpochKeeper = *epochmodulekeeper.NewKeeper(
    appCodec,
    keys[epochmoduletypes.StoreKey],
    keys[epochmoduletypes.MemStoreKey],
    app.GetSubspace(epochmoduletypes.ModuleName),
  ).SetHooks(epochmoduletypes.NewMultiEpochHooks(
    app.MintKeeper.Hooks()))
  ...
}
```

## Events

The x/epoch module emits the following events:

new_epoch:

- epoch_number: The new epoch's epoch number.
- epoch_time: The new epoch's start time.
- epoch_height: The height at which the new epoch was initiated.

## Parameters

The `x/epoch` module does not contain any parameters.
