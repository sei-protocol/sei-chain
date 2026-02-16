# SIP: Add EIP-2537 BLS12-381 Precompiles

## Abstract
This proposal introduces seven precompiled contracts to the Sei EVM for BLS12-381 curve operations, matching the Ethereum Pectra upgrade (EIP-2537). This enables Sei smart contracts to natively verify Ethereum Beacon Chain validator signatures, unlocking trustless light-client bridges between Sei and Ethereum.

## Motivation
Ethereum's Consensus Layer (Beacon Chain) uses BLS12-381 for validator signatures. Without native curve support, verifying these signatures on-chain is prohibitively expensive. Adding EIP-2537 precompiles to Sei enables:

- **Trustless Ethereum Bridge**: Sei contracts can directly verify Beacon Chain validator BLS signatures and sync committee attestations, enabling a fully trustless light-client bridge without relying on multisigs or oracle committees.
- **ZK Proof Verification**: Supports ZK-SNARKs (Groth16/PLONK) over the BLS12-381 curve, enabling privacy-preserving DeFi protocols and cross-chain state proofs.
- **Signature Aggregation**: Batch-verify thousands of BLS signatures in a single on-chain operation, reducing gas costs for multi-party protocols.
- **Pectra Compatibility**: Aligns Sei's EVM with Ethereum's Pectra upgrade, ensuring cross-chain tooling and contracts work seamlessly on both chains.

## Specification
The implementation strictly follows the [EIP-2537 specification](https://eips.ethereum.org/EIPS/eip-2537).

Seven precompiles at addresses `0x0b` through `0x11`:

| Address | Operation | Input | Output | Gas |
| :--- | :--- | :--- | :--- | :--- |
| `0x0b` | `BLS12_G1ADD` | 256 bytes (2 G1 points) | 128 bytes | 375 |
| `0x0c` | `BLS12_G1MSM` | 160*k bytes (k point-scalar pairs) | 128 bytes | variable |
| `0x0d` | `BLS12_G2ADD` | 512 bytes (2 G2 points) | 256 bytes | 600 |
| `0x0e` | `BLS12_G2MSM` | 288*k bytes (k point-scalar pairs) | 256 bytes | variable |
| `0x0f` | `BLS12_PAIRING_CHECK` | 384*k bytes (k G1-G2 pairs) | 32 bytes | 32600*k + 37700 |
| `0x10` | `BLS12_MAP_FP_TO_G1` | 64 bytes (field element) | 128 bytes | 5500 |
| `0x11` | `BLS12_MAP_FP2_TO_G2` | 128 bytes (Fp2 element) | 256 bytes | 23800 |

Key details:
- Points use **uncompressed encoding** (128 bytes for G1, 256 bytes for G2) in big-endian.
- G1MSM/G2MSM handle both single scalar multiplication (k=1) and multi-scalar multiplication with a discount table per EIP-2537.
- All precompiles accept **raw calldata** (no ABI encoding), matching Ethereum's precompile calling convention.
- Input validation includes field modulus range checks, on-curve checks, and subgroup checks where required.

## Rationale
The implementation wraps go-ethereum's native EIP-2537 precompiles (using the audited `gnark-crypto` library), ensuring byte-for-byte compatibility with Ethereum's Pectra execution spec tests. This avoids reimplementing complex curve arithmetic and inherits go-ethereum's security guarantees.

## Backwards Compatibility
These are new precompiles at addresses `0x0b`-`0x11`, which were previously unused in Sei's EVM. Existing Sei precompiles occupy the `0x1001`+ address range and are unaffected.

## Security Considerations
- The underlying `gnark-crypto` BLS12-381 implementation is formally verified and used in production by multiple Ethereum execution clients.
- Gas costs follow EIP-2537's pricing model with MSM discount tables, preventing DoS via expensive operations.
- All inputs are validated: field elements must be less than the BLS12-381 modulus, points must be on-curve, and subgroup membership is enforced for MSM and pairing operations.
