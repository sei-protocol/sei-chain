#!/bin/bash

# Sovereign Authorship Lock Script
# Seals RFCs, README, and LICENSE with SHA256 digest and optional GPG signing

set -e

echo "🔐 Starting Sovereign Authorship Lock..."

FILES=(
  "RFC-002_SeiKinSettlement.md"
  "RFC-003_Authorship_Licensing.md"
  "RFC-004_Vault_Enforcement.md"
  "RFC-005_Fork_Escrow_Terms.md"
  "LICENSE_Sovereign_Attribution"
  "README_SeiKin_RFC_Attribution.md"
)

CHECKSUM_FILE="integrity-checksums.txt"
MANIFEST_FILE="sovereign-seal.json"

# Step 1: Generate checksums
echo "📦 Generating SHA-256 checksums..."
sha256sum "${FILES[@]}" > "$CHECKSUM_FILE"
echo "✅ Checksums written to $CHECKSUM_FILE"

# Step 2: Sign checksums (optional)
if command -v gpg > /dev/null; then
  echo "✍️  Signing checksums with GPG..."
  gpg --clearsign "$CHECKSUM_FILE"
  echo "🔏 Signed: $CHECKSUM_FILE.asc"
else
  echo "⚠️  GPG not found — skipping signature"
fi

# Step 3: Create manifest JSON
echo "🧾 Building manifest $MANIFEST_FILE..."
echo "{" > "$MANIFEST_FILE"
for file in "${FILES[@]}"; do
  HASH=$(sha256sum "$file" | awk '{print $1}')
  MODIFIED=$(stat -c %y "$file" | cut -d'.' -f1)
  echo "  \"$file\": {" >> "$MANIFEST_FILE"
  echo "    \"sha256\": \"$HASH\"," >> "$MANIFEST_FILE"
  echo "    \"timestamp\": \"$MODIFIED\"" >> "$MANIFEST_FILE"
  echo "  }," >> "$MANIFEST_FILE"
done
sed -i '$ s/,$//' "$MANIFEST_FILE"
echo "}" >> "$MANIFEST_FILE"
echo "✅ Manifest created."

# Step 4: Git commit and tag
echo "📁 Committing to Git..."
git add "${FILES[@]}" "$CHECKSUM_FILE" "$CHECKSUM_FILE.asc" "$MANIFEST_FILE" 2>/dev/null || true
git commit -m "🔏 Sovereign Authorship Lock: RFCs + Manifest + License"
git tag v1.0-authorship-lock

echo "🚀 Sovereign authorship lock complete and tagged."
echo "🔐 To publish:"
echo "  git push origin main && git push origin v1.0-authorship-lock"
