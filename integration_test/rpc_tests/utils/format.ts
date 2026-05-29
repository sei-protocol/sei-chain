/**
 * Shared format matchers for JSON-RPC response validation.
 *
 * These encode the canonical Ethereum JSON-RPC encodings (QUANTITY, DATA, address)
 * so individual specs can assert "this is a well-formed X" without re-deriving the
 * regex each time. Keep them strict — a loose matcher hides real schema regressions.
 */

/**
 * Canonical QUANTITY: 0x-prefixed, lower-case hex, no leading zeros (except "0x0").
 * Per the Ethereum JSON-RPC spec, quantities must be minimally encoded.
 */
export const HEX_QUANTITY = /^0x(0|[1-9a-f][0-9a-f]*)$/;

/** 20-byte address, 0x-prefixed. Case-insensitive (covers checksummed + lowercase). */
export const ADDRESS = /^0x[0-9a-fA-F]{40}$/;

/** Lower-cased 20-byte address (some endpoints return non-checksummed addresses). */
export const ADDRESS_LOWER = /^0x[0-9a-f]{40}$/;

/** Arbitrary 0x-prefixed byte string with an even number of nibbles. */
export const HEX_DATA = /^0x([0-9a-fA-F]{2})*$/;

export const isHexQuantity = (v: unknown): v is string =>
    typeof v === 'string' && HEX_QUANTITY.test(v);

export const isAddress = (v: unknown): v is string =>
    typeof v === 'string' && ADDRESS.test(v);

export const isHexData = (v: unknown): v is string =>
    typeof v === 'string' && HEX_DATA.test(v);
