<!--
order: 2
-->

# Integrating IBC Middleware into a Chain

Learn how to integrate IBC middleware(s) with a base application to your chain. The following document only applies for Cosmos SDK chains.

If the middleware is maintaining its own state and/or processing SDK messages, then it should create and register its SDK module **only once** with the module manager in `app.go`.

All middleware must be connected to the IBC router and wrap over an underlying base IBC application. An IBC application may be wrapped by many layers of middleware, only the top layer middleware should be hooked to the IBC router, with all underlying middlewares and application getting wrapped by it.

The order of middleware **matters**, function calls from IBC to the application travel from top-level middleware to the bottom middleware and then to the application. Function calls from the application to IBC goes through the bottom middleware in order to the top middleware and then to core IBC handlers. Thus the same set of middleware put in different orders may produce different effects.

### Example integration

```go
// app.go

// middleware 1 and middleware 3 are stateful middleware, 
// perhaps implementing separate sdk.Msg and Handlers
mw1Keeper := mw1.NewKeeper(storeKey1)
mw3Keeper := mw3.NewKeeper(storeKey3)

// Only create App Module **once** and register in app module
// if the module maintains independent state and/or processes sdk.Msgs
app.moduleManager = module.NewManager(
    ...
    mw1.NewAppModule(mw1Keeper),
    mw3.NewAppModule(mw3Keeper),
    transfer.NewAppModule(transferKeeper),
    custom.NewAppModule(customKeeper)
)

mw1IBCModule := mw1.NewIBCModule(mw1Keeper)
mw2IBCModule := mw2.NewIBCModule() // middleware2 is stateless middleware
mw3IBCModule := mw3.NewIBCModule(mw3Keeper)

scopedKeeperTransfer := capabilityKeeper.NewScopedKeeper("transfer")
scopedKeeperCustom1 := capabilityKeeper.NewScopedKeeper("custom1")
scopedKeeperCustom2 := capabilityKeeper.NewScopedKeeper("custom2")

// NOTE: IBC Modules may be initialized any number of times provided they use a separate
// scopedKeeper and underlying port.

// initialize base IBC applications
// if you want to create two different stacks with the same base application,
// they must be given different scopedKeepers and assigned different ports.
transferIBCModule := transfer.NewIBCModule(transferKeeper, scopedKeeperTransfer)
customIBCModule1 := custom.NewIBCModule(customKeeper, scopedKeeperCustom1, "portCustom1")
customIBCModule2 := custom.NewIBCModule(customKeeper, scopedKeeperCustom2, "portCustom2")

// create IBC stacks by combining middleware with base application
// NOTE: since middleware2 is stateless it does not require a Keeper
// stack 1 contains mw1 -> mw3 -> transfer
stack1 := mw1.NewIBCModule(mw1Keeper, mw3.NewIBCModule(mw3Keeper, transferIBCModule))
// stack 2 contains mw3 -> mw2 -> custom1
stack2 := mw3.NewIBCModule(mw3Keeper, mw3.NewIBCModule(customIBCModule1))
// stack 3 contains mw2 -> mw1 -> custom2
stack3 := mw2.NewIBCModule(mw1.NewIBCModule(mw1Keeper, customIBCModule2))

// associate each stack with the moduleName provided by the underlying scopedKeeper
ibcRouter := porttypes.NewRouter()
ibcRouter.AddRoute("transfer", stack1)
ibcRouter.AddRoute("custom1", stack2)
ibcRouter.AddRoute("custom2", stack3)
app.IBCKeeper.SetRouter(ibcRouter)
```

