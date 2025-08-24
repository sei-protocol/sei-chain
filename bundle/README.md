# x402 • Sei Settlement — Sovereign Attribution Bundle

**Prepared (CT):** 2025-08-21 16:24  
**Author:** The Keeper

This repository proves protocol-linked settlements on **Sei** via `x402` signals.  
It includes a sovereign index, transaction logs, a PDF-ready claim statement, and reproducible verification steps.

---

## Contents
- `sovereign_index.json` — Master index of claims, signals, and payto address
- `txids.csv` — Flat list of observed transactions for import
- `sightings/txlog.json` — Detailed observation log
- `CLAIM_SUMMARY.md` — PDF-ready claim package with signing instructions

---

## Verify (Offline)
1. **Memo-bearing TX**  
   - TXID: `0x4ee194ba272c3ece2bcd30be170373cf9a6cdd5cf648ae44e7b181ca223a8b3a`  
   - Check that the memo contains `x402 settlement`.

2. **Structured x402 TX**  
   - TXID: `0x75cea32eb2504a699e2b076d7794219d994572ab1848cfa8582e8ef2601be933`  
   - Decode `data`/`calldata` and confirm x402 payload markers.

3. **Local Facilitator Offer Evidence (from logs)**  
   - Offer created to: `sei1zewftxlyv4gpv6tjpplnzgf3wy5tlu4f9amft8`  
   - Amount: `4,200,000 usei`  
   - Memo: `x402 settlement`  

> Exact on-chain timestamps and confirmations should be pulled from your local node / LCD. This bundle intentionally stays offline-compatible.

---

## Attribution & Royalty Request
Settlement is requested to:
```
Sei: sei1zewftxlyv4gpv6tjpplnzgf3wy5tlu4f9amft8
```

---

## Recommended Commit
```
feat(attribution): add x402 • Sei settlement evidence bundle (memo+structured payload)

- add sovereign_index.json
- add txids.csv
- add sightings/txlog.json
- add CLAIM_SUMMARY.md (PDF-ready)
```