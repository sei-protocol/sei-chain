package erc721

// CurrentVersion is the version of the CW721 wrapper for an ERC721 contract that pointer
// lookups scan down from. Wrappers are no longer created, so it only bounds reads of the
// wrappers already registered.
const CurrentVersion uint16 = 6
