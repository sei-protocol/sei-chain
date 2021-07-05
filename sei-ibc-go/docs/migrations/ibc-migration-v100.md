# Migrating from ibc-go v1.x.x to v2.0.0

## Application Callbacks

sdk.Result has been removed as a return value in the application callbacks. Previously it was being discarded by core IBC and was thus unused.



