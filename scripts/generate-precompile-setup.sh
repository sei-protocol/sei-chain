#!/bin/bash

set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "Usage: $0 <module_name> <new_tag>"
  exit 1
fi

MODULE_NAME="$1"
VERSIONS_FILE="versions"
OUTPUT_FILE="setup.go"

NEW_TAG="$2"
if [ "$(tail -n 1 "$VERSIONS_FILE")" = "$NEW_TAG" ]; then
  lines=$(wc -l < "$VERSIONS_FILE")
  head -n $((lines - 1)) "$VERSIONS_FILE" > temp.txt
  mv temp.txt "$VERSIONS_FILE"
fi

if [ ! -f "$MODULE_NAME".go ]; then
  echo "Latest precompile not found: $MODULE_NAME".go
  exit 0
fi

echo "Generating $OUTPUT_FILE for module: $MODULE_NAME"

# Start writing the setup.go file
cat > "$OUTPUT_FILE" <<EOF
package $MODULE_NAME

import (
	"github.com/ethereum/go-ethereum/core/vm"
EOF

# Import each version as alias
while IFS= read -r version; do
  [[ -z "$version" ]] && continue
  clean="${version//./}"  # e.g., v552
  echo "	$MODULE_NAME$clean \"github.com/sei-protocol/sei-chain/precompiles/$MODULE_NAME/legacy/$clean\"" >> "$OUTPUT_FILE"
done < "$VERSIONS_FILE"

# Add utils import and function body
cat >> "$OUTPUT_FILE" <<EOF
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

func GetVersioned(latestUpgrade string, keepers utils.Keepers) utils.VersionedPrecompiles {
	return utils.VersionedPrecompiles{
		latestUpgrade: check(NewPrecompile(keepers)),
EOF

# Add versioned entries
while IFS= read -r version; do
  [[ -z "$version" ]] && continue
  clean="${version//./}"
  echo "		\"$version\":      check($MODULE_NAME$clean.NewPrecompile(keepers))," >> "$OUTPUT_FILE"
done < "$VERSIONS_FILE"

# Finish file
cat >> "$OUTPUT_FILE" <<EOF
	}
}

func check(p vm.PrecompiledContract, err error) vm.PrecompiledContract {
	if err != nil {
		panic(err)
	}
	return p
}
EOF

echo "Done: $OUTPUT_FILE generated."