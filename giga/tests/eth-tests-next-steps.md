# EVM Executor Test Suite Selection Plan

## Context
- **Project**: Sei Labs new EVM-only executor (giga)
- **Target Fork**: Cancun (without blob transactions / EIP-4844)
- **Scope**: Both transaction and block-level processing
- **Architecture**: RLP/Trie handled by Sei chain layer, executor focuses on EVM execution
- **Already Completed**: GeneralStateTests (transaction-level tests)

---

## Recommended Test Suites

### Run These

| Suite | Files | Purpose | Notes |
|-------|-------|---------|-------|
| **BlockchainTests** | 347 | Block import, multi-TX execution, chain state | Critical for block-level processing |
| **TransactionTests** | 212 | TX validity, intrinsic gas, signature validation | Filter out EIP-4844 blob TX tests |

### Skip These

| Suite | Reason |
|-------|--------|
| **TrieTests** | Sei handles Merkle Trie |
| **RLPTests** | Sei handles RLP encoding |
| **DifficultyTests** | PoW-specific, not relevant |
| **PoWTests** | PoW-specific, not relevant |
| **EOFTests** | Pectra fork feature, not in Cancun |
| **KeyStoreTests** | Client-side key management, not executor concern |
| **GenesisTests** | Chain initialization, likely Sei's responsibility |

### Optional / Partial

| Suite | Consideration |
|-------|---------------|
| **BasicTests** | `crypto.json` and `keyaddrtest.json` may be useful if executor handles any crypto directly |
| **ABITests** | Only if executor has ABI encoding logic (typically handled by contracts/clients) |

---

## BlockchainTests Structure

### Format Comparison: BlockchainTests vs GeneralStateTests

| Aspect | BlockchainTests | GeneralStateTests |
|--------|-----------------|-------------------|
| **Focus** | Full blockchain execution | Individual transactions |
| **Structure** | Complete blocks with headers | Single TX with environment |
| **State check** | `postState` or `postStateHash` | `post` with fork-specific results |
| **Input** | Genesis + array of blocks | Pre-state + single transaction |
| **Validates** | Block import, chain rules, multi-TX | Transaction execution only |

### BlockchainTest JSON Structure
```json
{
  "testName": {
    "_info": { /* metadata */ },
    "network": "Cancun",
    "genesisBlockHeader": { /* block 0 header */ },
    "genesisRLP": "0x...",
    "blocks": [
      {
        "blockHeader": { /* header fields */ },
        "transactions": [ /* array of TXs */ ],
        "uncleHeaders": [],
        "withdrawals": [ /* validator withdrawals */ ],
        "rlp": "0x..."
      }
    ],
    "postState": { /* expected final state */ },
    "lastblockhash": "0x...",
    "pre": { /* initial accounts/state */ }
  }
}
```

### Directory Structure
```
BlockchainTests/
├── ValidBlocks/           # 15 categories - blocks that should import successfully
│   ├── bcExample/         # Basic examples per fork
│   ├── bcEIP1559/         # Fee market tests
│   ├── bcEIP4844-blobtransactions/  # EXCLUDE (blob tests)
│   ├── bcEIP1153-transientStorage/  # TSTORE/TLOAD tests
│   ├── bcStateTests/      # State transition tests (65 subdirs)
│   └── ...
├── InvalidBlocks/         # 12 categories - blocks that should fail
│   ├── bcInvalidHeaderTest/
│   ├── bc4895-withdrawals/
│   └── ...
└── .meta/index.json       # Test index with fork info
```

---

## Filtering Out EIP-4844 Blob Tests

### Directories to Exclude (14 total test files)

**GeneralStateTests** (13 files):
```
GeneralStateTests/Cancun/stEIP4844-blobtransactions/
```

**BlockchainTests** (1 file):
```
BlockchainTests/ValidBlocks/bcEIP4844-blobtransactions/
```

### Glob Patterns for Exclusion
```bash
# Exclude these patterns:
**/stEIP4844-blobtransactions/**
**/bcEIP4844-blobtransactions/**
```

### Keep These Cancun Tests
```
GeneralStateTests/Cancun/stEIP1153-transientStorage/  # TSTORE/TLOAD
GeneralStateTests/Cancun/stEIP5656-MCOPY/             # MCOPY opcode
```

### Content Markers (if deeper filtering needed)
Tests containing blob functionality have these markers:
- Transaction type `0x03`
- Fields: `blobVersionedHashes`, `maxFeePerBlobGas`
- Opcodes: `BLOBHASH` (0x49), `BLOBBASEFEE` (0x4a)
- Header fields: `blobGasUsed`, `excessBlobGas`

---

## Cancun EIPs to Test (without blobs)

| EIP | Feature | Test Location |
|-----|---------|---------------|
| EIP-1153 | Transient storage (TSTORE/TLOAD) | `stEIP1153-transientStorage/` |
| EIP-5656 | MCOPY instruction | `stEIP5656-MCOPY/` |
| EIP-6780 | SELFDESTRUCT changes | Various state tests |
| EIP-4788 | Beacon block root in EVM | BlockchainTests |

---

## Verification
After running each suite, compare results against known-passing implementations (geth, reth) at the same fork level.
