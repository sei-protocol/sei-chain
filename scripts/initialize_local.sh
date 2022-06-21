# min go compiler version >=1.18.2
# gvm use go1.18.2
# build seid
go build -o build/seid ./cmd/seid/
# bootstrap from scratch
rm -rf ~/.sei/
rm -rf ~/test_accounts/
# init chain
./build/seid init demo --chain-id sei
test_account_name=alice
# add test_account_name to keys
./build/seid keys add $test_account_name
./build/seid add-genesis-account $(./build/seid keys show $test_account_name -a) 100000000000000000000sei
# generate genesis tx
./build/seid gentx $test_account_name 70000000000000000000sei --chain-id sei
./build/seid collect-gentxs
cat ~/.sei/config/genesis.json | jq '.app_state["crisis"]["constant_fee"]["denom"]="sei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["deposit_params"]["min_deposit"][0]["denom"]="sei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["mint"]["params"]["mint_denom"]="sei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["staking"]["params"]["bond_denom"]="sei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
./build/seid start --trace
