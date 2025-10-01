#!/bin/bash

# Sovereign Authorship Lock Script
# Seals RFCs, README, and LICENSE with SHA256 digest and optional GPG signing

set -e

echo "🔐 Starting Sovereign Authorship Lock..."

# Define files to hash
FILES=(
  "RFC-002_SeiKinSettlement.md"
  "RFC-003_Authorship_Licensing.md"
  "RFC-004_Vault_Enforcement.md"
  "RFC-005_Fork_Escrow_Terms.md"
  "LICENSE_Sovereign_Attribution"
  "README_SeiKin_RFC_Attribution.md"
)

CHECKSUM_FILE="integrity-checksums.txt"

# Step 1: Generate checksums
echo "📦 Generating SHA-256 checksums..."
sha256sum "${FILES[@]}" > "$CHECKSUM_FILE"
echo "✅ Checksums written to $CHECKSUM_FILE"

# Step 2 (optional): Sign with GPG if available
if command -v gpg > /dev/null; then
  echo "✍️  Signing checksums with GPG..."
  gpg --clearsign "$CHECKSUM_FILE"
  echo "🔏 Signed: $CHECKSUM_FILE.asc"
else
  echo "⚠️  GPG not found — skipping signature"
fi

# Step 3: Git commit and tag
echo "📁 Committing sealed files to Git..."
git add "${FILES[@]}" "$CHECKSUM_FILE" "$CHECKSUM_FILE.asc" 2>/dev/null || true
git commit -m "🔏 Sovereign Authorship Lock: RFCs 002–005 + License + Attribution Notarized"
git tag v1.0-authorship-lock

echo "🚀 Tag 'v1.0-authorship-lock' created."
echo "✅ Sovereign authorship lock complete."

# Optional: push instructions
echo -e "\nTo publish:"
echo "  git push origin main && git push origin v1.0-authorship-lock"
