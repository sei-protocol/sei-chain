#!/bin/bash
set -euo pipefail

echo "🔗 Initiating Codex ↔ Omega KinLink Bridge"

# Log initial session state
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
UUID=$(uuidgen || cat /proc/sys/kernel/random/uuid)

echo "KinLink Initiated: $TIMESTAMP [$UUID]" >> codex/omega/kinlink.log

# Touch build tracker
touch codex/omega/state/build_active.flag

# Create Kin metadata file for this payload
cat <<EOM > codex/omega/state/payload_001_metadata.json
{
  "id": "$UUID",
  "name": "kin_sei_link",
  "timestamp": "$TIMESTAMP",
  "status": "active",
  "codex_path": "codex/omega/payloads/kin_sei_link.sh",
  "description": "Initial Codex–Omega link bridge payload for infinite build"
}
EOM

echo "✅ KinLink payload registered. You may now begin adding child payloads."

# Optional: trigger next stub
echo "#!/bin/bash" > codex/omega/payloads/next_payload.sh
echo "echo 'Payload 002 not yet defined.'" >> codex/omega/payloads/next_payload.sh
chmod +x codex/omega/payloads/next_payload.sh
