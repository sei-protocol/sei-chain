The oracle module is deprecated.
All oracle messages and queries return `ErrOracleDeprecated`.
The oracle store remains part of application state for compatibility.
The oracle MidBlocker and EndBlocker remain registered and active.
An upgrade will remove the module and its persisted state in the future.
