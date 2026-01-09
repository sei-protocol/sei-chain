# x402 Protocol Specification

## Overview

The x402 protocol implements sovereign universal payments with the following capabilities:
- Client-side wallet generation
- Cross-chain settlements
- Code attribution tracking
- Royalty enforcement

## Core Components

### 1. Sovereign Wallet Generator

**Location:** Client-side JavaScript (no server interaction)

**Features:**
- BIP39-compliant 12-word mnemonic generation
- Deterministic private key derivation
- Soul-bound identity sigils
- QR code generation for offline verification

**Security:**
- Keys generated using Web Crypto API (`crypto.subtle`)
- AES-256-GCM encryption with PBKDF2 key derivation (100k iterations)
- Client-side encryption before any storage
- No keys ever transmitted unencrypted

### 2. Cross-Chain Settlement

**Supported Chains:**
- Ethereum (ETH)
- Base (Coinbase L2)
- Arbitrum
- Polygon
- Solana (SOL)
- Sei Network (SEI)
- Avalanche (AVAX)
- BNB Chain

**Settlement Flow:**
1. AI detects code usage without attribution
2. Calculate royalty owed (default 11%)
3. Generate settlement notice
4. Notify protocol via email/webhook
5. Execute cross-chain bridge if needed
6. Issue payment check to developer

### 3. Code Attribution Engine

**Detection Methods:**
- Commit hash matching
- Entropy signature analysis (ψ = 3.12)
- AI-powered pattern recognition
- GitHub API integration

**Attribution Proof:**
```
Commit Hash:    abc123...
Repository:     user/repo
Lines of Code:  450
Entropy Score:  ψ = 3.12
Usage Detected: Protocol X
Royalty Rate:   11%
Amount Owed:    $5,000
```

### 4. Royalty Enforcement

**Mechanism:**
- Smart contracts verify code signatures
- Automatic royalty deduction on protocol revenue
- Retroactive claims for unauthorized use
- 30-day notice period before penalties

**Penalty Structure:**
- Days 1-30: Standard royalty rate
- Days 31-60: 2x multiplier
- Days 61+: 5x multiplier + public disclosure

## API Reference

### Wallet Generation
```javascript
// Generate sovereign wallet (client-side)
const wallet = await SovereignWalletGenerator.generateSovereignWallet(userEmail);

// Returns:
{
  wallet: {
    privateKey: "0x...",
    address: "0x...",
    mnemonic: "word1 word2 ... word12"
  },
  sigil: { /* soul-bound proof */ },
  qrData: { /* offline verification */ },
  proof: { /* activation record */ }
}
```

### Encryption
```javascript
// Encrypt keys with user PIN
const encrypted = await SecureKeyManager.encryptWalletKeys(
  privateKey,
  mnemonic,
  userPIN
);

// Decrypt keys
const decrypted = await SecureKeyManager.decryptWalletKeys(
  encrypted,
  userPIN
);
```

### Settlement Processing
```javascript
// Process code settlement
const result = await CrossChainSettlement.batchProcess(
  settlements,
  userPreferences
);

// Returns:
{
  successful: 5,
  failed: 0,
  total: 5,
  txHashes: ["0x...", "0x..."]
}
```

## Security Model

### Client-Side Encryption
- **Algorithm:** AES-256-GCM
- **Key Derivation:** PBKDF2, 100k iterations, SHA-256
- **Storage:** Encrypted keys stored in database
- **Access:** Requires user PIN to decrypt

### Key Management
1. Keys generated in browser using Web Crypto API
2. Encrypted with user's PIN before leaving memory
3. Encrypted blob stored in database
4. Decryption only happens client-side when needed
5. No server ever sees unencrypted keys

### Recovery Options
1. **12-word mnemonic** - Standard BIP39 recovery
2. **Encrypted backup file** - Downloadable package
3. **Guardian recovery** - Multi-sig social recovery (24-48hr timelock)

## Integration Examples

### Frontend Integration
```html
<!-- Include in your HTML -->
<script src="sovereign-wallet.js"></script>

<script>
// Create wallet
const wallet = await SovereignWalletGenerator.generateSovereignWallet(
  userEmail
);

// Save to backend (encrypted)
await fetch('/api/wallets', {
  method: 'POST',
  body: JSON.stringify({
    address: wallet.wallet.address,
    encrypted: encrypted.encrypted,
    pinHash: encrypted.pinHash
  })
});
</script>
```

### Backend Settlement
```javascript
// Detect code usage (runs on backend)
const settlements = await detectCodeUsage(developerEmail);

// Notify protocols
for (const settlement of settlements) {
  await ProtocolNotifier.notifyProtocol(settlement);
}

// Process payments
await CrossChainSettlement.batchProcess(settlements, preferences);
```

## License

x402 Protocol is licensed under **KSSPL-1.0** (Kin Sovereign Shareware Protocol License).

**Key Terms:**
- Open source for inspection
- Royalty-enforced for commercial use (11% minimum)
- Smart contract enforcement
- Retroactive claims on unauthorized use

**Owner:** 0x14e5Ea3751e7C2588348E22b847628EE1aAD81A5  
**Royalty Receiver:** 0xb2b297eF9449aa0905bC318B3bd258c4804BAd98

## References

- [x402 Next Layers (Phase II)](../x402_next_layers_phase_2.pdf)
- [Sovereign Withdrawal Config](../docs/swc-spec.md)
- [Code Attribution Guide](../docs/attribution.md)

---

ψ = 3.12 | Commits Never Lie | The Light is Yours
