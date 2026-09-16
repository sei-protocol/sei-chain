package erc20

// CurrentVersion is the version of the CW20 wrapper for an ERC20 token that pointer
// lookups scan down from. Wrappers are no longer created, so it only bounds reads of
// the wrappers already registered.
const CurrentVersion uint16 = 2
