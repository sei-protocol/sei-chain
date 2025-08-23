# x402 • Sei Settlement — Claim Summary
**Author:** The Keeper  
**Prepared (CT):** 2025-08-21 16:24

---

## Statement
I assert authorship and contribution to the `x402` protocol flows and their operationalization on Sei. The following transactions and offer logs constitute **direct** and **indirect** settlement signals tied to my work:

- **Direct (Memo Match):**  
  TXID `0x4ee194...3a` — memo contains `x402 settlement`.
- **Indirect (Structured Payload):**  
  TXID `0x75cea3...33` — decoded data includes an `x402` payload.
- **Offer Evidence (Local Facilitator):**  
  To: `sei1zewftxlyv4gpv6tjpplnzgf3wy5tlu4f9amft8`, Amount: `4,200,000 usei`, Memo: `x402 settlement`, From (EVM origin): `0xb2b297eF9449aa0905bC318B3bd258c4804BAd98`.

## Requested Action
Record attribution and settle outstanding contributor compensation / royalties to:

```
Sei Address: sei1zewftxlyv4gpv6tjpplnzgf3wy5tlu4f9amft8
```

## Evidence Attachments
- `sovereign_index.json`
- `sightings/txlog.json`
- `txids.csv`

---

## How to Verify (Offline / Deterministic)
1. **Memo TX Decode:**  
   Use your local Sei LCD or full node to fetch TX `0x4ee1...3a` and verify the memo field equals `x402 settlement`.

2. **Structured Payload Decode:**  
   For TX `0x75ce...33`, extract the `data`/`calldata` bytes and search for `x402` tokenized markers as per your decoder.

3. **Offer Proof:**  
   Compare the included JSON line with your server logs if you ran the local facilitator.

> These checks can be performed in an air-gapped environment; no reliance on public explorers is required.

---

## Optional: EIP-191 Signing (Proves Author Claim)
**Message:**
```
I, The Keeper, claim authorship and contributor rights for x402 Sei integrations and request settlement to sei1zewftxlyv4gpv6tjpplnzgf3wy5tlu4f9amft8.
Prepared CT: 2025-08-21 16:24
Bundle Hash Intent: (compute locally)
```

Sign with your chosen key and attach the signature in your repo or PDF appendix.

---

## QR Payload Strings (Generate Locally)
For convenience, you can create QR codes from the following plain strings:
```
x402:sei:payto=sei1zewftxlyv4gpv6tjpplnzgf3wy5tlu4f9amft8
x402:sei:tx=0x4ee194ba272c3ece2bcd30be170373cf9a6cdd5cf648ae44e7b181ca223a8b3a
x402:sei:tx=0x75cea32eb2504a699e2b076d7794219d994572ab1848cfa8582e8ef2601be933
```
Generate offline with:
```
qrencode -o payto.png "x402:sei:payto=sei1zewftxlyv4gpv6tjpplnzgf3wy5tlu4f9amft8"
```

---

## Contact
Attribution and settlement acknowledgments may reference **The Keeper** and the `x402 • Sei` contribution line.

*Prepared in sovereignty and in truth.*