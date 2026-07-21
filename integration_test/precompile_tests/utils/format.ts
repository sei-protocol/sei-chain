/**
 * Shared format matchers for response validation. Keep them strict — a loose
 * matcher hides real schema regressions.
 */

/** 20-byte address, 0x-prefixed. Case-insensitive (covers checksummed + lowercase). */
export const ADDRESS = /^0x[0-9a-fA-F]{40}$/;

/** Arbitrary 0x-prefixed byte string with an even number of nibbles. */
export const HEX_DATA = /^0x([0-9a-fA-F]{2})*$/;

/** 32-byte hash (tx hash, block hash, …), 0x-prefixed. Case-insensitive. */
export const HASH32 = /^0x[0-9a-fA-F]{64}$/;

/** A bech32 `sei1…` account address. */
export const SEI_ADDRESS = /^sei1[02-9ac-hj-np-z]{38,58}$/;
