#/bin/bash

TOP_N=$1
TOP_N=${TOP_N:-10}

systemctl stop seid
echo "Dumping all wasm keys to ~/wasm_keys_dump.txt..."
iaviewer keys ~/.sei/data/application.db "s/k:wasm/" > ~/wasm_keys_dump.txt

systemctl start seid
echo "Generating top contracts to ~/top_contracts.txt..."
cat ~/wasm_keys_dump.txt |grep "^03" |cut -c 3-66 |sort |uniq -c |sort -nr |head -n "$TOP_N" > ~/top_contracts.txt

while IFS= read -r line; do
  count=$(echo "$line" | awk '{print $1}')
  contract_hex=$(echo "$line" | awk '{print $2}')
  contract_address=$(seid keys parse "$contract_hex" --output json |jq -r .formats[0])
  contract_label=$(seid query wasm contract "$contract_address" --output json |jq -r .contract_info.label)
  echo "$contract_address - $contract_label: $count"
done < ~/top_contracts.txt
