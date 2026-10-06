package erc1155

// CurrentVersion is the version of the CW1155 wrapper for an ERC1155 contract that
// pointer lookups scan down from. Wrappers are no longer created, so it only bounds
// reads of the wrappers already registered.
const CurrentVersion uint16 = 1
