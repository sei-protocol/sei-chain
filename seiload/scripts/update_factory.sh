#!/bin/bash

# Script to update the scenario factory with auto-generated contract entries
# Usage: ./update_factory.sh <ContractName1> <ContractName2> ...

set -e

FACTORY_FILE="generator/scenarios/factory.go"
TEMP_FILE=$(mktemp)

if [ ! -f "$FACTORY_FILE" ]; then
    echo "❌ Factory file not found: $FACTORY_FILE"
    exit 1
fi

echo "🔄 Updating scenario factory with contract entries..."

# Get all contract names passed as arguments
CONTRACT_NAMES=("$@")

if [ ${#CONTRACT_NAMES[@]} -eq 0 ]; then
    echo "⚠️  No contract names provided, factory will only contain manual entries"
fi

# Read the factory file and preserve manual entries
# Extract everything before the auto-generated section
sed -n '1,/DO NOT EDIT BELOW THIS LINE - AUTO-GENERATED CONTENT/p' "$FACTORY_FILE" > "$TEMP_FILE"

# Add auto-generated entries
for contract in "${CONTRACT_NAMES[@]}"; do
    echo "	${contract}: New${contract}Scenario," >> "$TEMP_FILE"
done

# Add the closing marker and everything after the auto-generated section
echo "" >> "$TEMP_FILE"
echo "	// DO NOT EDIT ABOVE THIS LINE - AUTO-GENERATED CONTENT" >> "$TEMP_FILE"

# Extract everything after the auto-generated section (functions, etc.)
sed -n '/DO NOT EDIT ABOVE THIS LINE - AUTO-GENERATED CONTENT/,$p' "$FACTORY_FILE" | tail -n +2 >> "$TEMP_FILE"

# Replace the original file
mv "$TEMP_FILE" "$FACTORY_FILE"

echo "✅ Updated factory with ${#CONTRACT_NAMES[@]} contract entries: ${CONTRACT_NAMES[*]}"
