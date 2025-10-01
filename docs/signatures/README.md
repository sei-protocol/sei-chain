# Integrity Signature Verification

The integrity checksum manifest is signed with the SeiKin RFC signing key so downstream users can confirm the bundle has not been tampered with.

## Fetch the public key

Download the ASCII-armored public key from the project's keyserver entry and verify the fingerprint matches before importing:

```bash
curl -L https://keys.openpgp.org/vks/v1/by-fingerprint/9464BC0965B729630789764AAA61DE3BF64D5D19 \
  -o docs/signatures/keeper-pubkey.asc

gpg --show-keys docs/signatures/keeper-pubkey.asc
```

The expected fingerprint is:

```
9464 BC09 65B7 2963 0789 764A AA61 DE3B F64D 5D19
```

Alternatively, you can fetch the key directly with GnuPG:

```bash
gpg --keyserver keys.openpgp.org --recv-keys 9464BC0965B729630789764AAA61DE3BF64D5D19
```

## Verify the signature

Once the key is imported, verify the checksum manifest signature:

```bash
gpg --import docs/signatures/keeper-pubkey.asc

gpg --verify docs/signatures/integrity-checksums.txt.asc
```

If the signature is valid you will see a `Good signature` message tied to the SeiKin RFC signing key fingerprint above.
