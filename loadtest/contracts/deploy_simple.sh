#!/bin/bash
echo -n Admin Key Name:
read keyname
echo
echo -n Chain ID:
read chainid
echo
echo -n seid binary:
read seidbin
echo
echo -n sei-chain directory:
read seihome
echo
echo -n contract count:
read count
echo

# Build all contracts
echo "Building contracts..."

cd $seihome/loadtest/contracts/simpleexec && cargo build --release --target wasm32-unknown-unknown

# Deploy all contracts
echo "Deploying contracts..."

cd $seihome/loadtest/contracts

valaddr=$(printf "12345678\n" | $seidbin keys show $(printf "12345678\n" | $seidbin keys show node_admin --output json | jq -r .address) --bech=val --output json | jq -r '.address')
sres=$(printf "12345678\n" | $seidbin tx staking delegate $valaddr 1000000000usei --from=$keyname --chain-id=$chainid -b block -y --fees 2000usei)

echo '{"title":"enable wasm","description":"enable wasm","message_dependency_mapping":[{"message_key":"cosmwasm.wasm.v1.MsgExecuteContract","access_ops":[{"access_type":"UNKNOWN","resource_type":"ANY","identifier_template":"*"},{"access_type":"COMMIT","resource_type":"ANY","identifier_template":"*"}],"dynamic_enabled": true}]}' > resource.json
res=$(printf "12345678\n" | $seidbin tx accesscontrol update-resource-dependency-mapping resource.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
proposalid=$(python3 parser.py proposal_id $res)
dres=$(printf "12345678\n" | $seidbin tx gov deposit $proposalid 10000000usei -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block)
vres=$(printf "12345678\n" | $seidbin tx gov vote $proposalid yes -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block)
for (( c=1; c<=$count; c++ ))
do
# store
storeres=$(printf "12345678\n" | $seidbin tx wasm store simpleexec/target/wasm32-unknown-unknown/release/simpleexec.wasm -y --from=$keyname --chain-id=$chainid --gas=50000000 --fees=1000000usei --broadcast-mode=block --output=json)
id=$(python3 parser.py code_id $storeres)

# instantiate
insres=$(printf "12345678\n" | $seidbin tx wasm instantiate $id '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
addr=$(python3 parser.py contract_address $insres)

rm -f wasm.json
echo '{"title":"enable wasm '$addr'","description":"enable wasm '$addr'","contract_address":"'$addr'","wasm_dependency_mapping":{"enabled":true,"access_ops":[{"access_type":"WRITE","resource_type":"KV_WASM","identifier_template":"'$addr'"},{"access_type":"COMMIT","resource_type":"ANY","identifier_template":"*"}]}}' > wasm.json
res=$(printf "12345678\n" | $seidbin tx accesscontrol update-wasm-dependency-mapping wasm.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
proposalid=$(python3 parser.py proposal_id $res)
dres=$(printf "12345678\n" | $seidbin tx gov deposit $proposalid 10000000usei -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block)
vres=$(printf "12345678\n" | $seidbin tx gov vote $proposalid yes -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block)

echo $addr
done

sleep 90

ures=$(printf "12345678\n" | $seidbin tx staking unbond $valaddr 1000000000usei --from=$keyname --chain-id=$chainid -b block -y --fees 2000usei)