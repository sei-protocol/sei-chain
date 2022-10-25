# min go compiler version >=1.18.2
# gvm use go1.18.2
# build seid
go build -o build/seid ./cmd/seid/
# bootstrap from scratch
rm -rf ~/.sei/
rm -rf ~/test_accounts/
./build/seid tendermint unsafe-reset-all
# init chain
./build/seid init demo --chain-id sei
test_account_name=alice
# add test_account_name to keys
./build/seid keys add $test_account_name
printf '00000000\n' | ./build/seid add-genesis-account $(./build/seid keys show $test_account_name -a) 100000000000000000000usei
# generate genesis tx
printf '00000000\n' | ./build/seid gentx $test_account_name 70000000000000000000usei --chain-id sei
sed -i 's/mode = "full"/mode = "validator"/g' $HOME/.sei/config/config.toml
sed -i 's/indexer = \["null"\]/indexer = \["kv"\]/g' $HOME/.sei/config/config.toml
KEY=$(jq '.pub_key' ~/.sei/config/priv_validator_key.json -c)
jq '.validators = [{}]' ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
jq '.validators[0] += {"power":"70000000000000"}' ~/.sei/config/tmp_genesis.json > ~/.sei/config/tmp_genesis_2.json
jq '.validators[0] += {"pub_key":'$KEY'}' ~/.sei/config/tmp_genesis_2.json > ~/.sei/config/tmp_genesis_3.json
mv ~/.sei/config/tmp_genesis_3.json ~/.sei/config/genesis.json && rm ~/.sei/config/tmp_genesis.json && rm ~/.sei/config/tmp_genesis_2.json
./build/seid collect-gentxs
cat ~/.sei/config/genesis.json | jq '.app_state["crisis"]["constant_fee"]["denom"]="usei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["deposit_params"]["min_deposit"][0]["denom"]="usei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["mint"]["params"]["mint_denom"]="usei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["staking"]["params"]["bond_denom"]="usei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["deposit_params"]["max_deposit_period"]="30s"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["voting_params"]["voting_period"]="30s"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.consensus_params["block"]["time_iota_ms"]="30"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
printf '00000000\n' | ./build/seid start --log_level warn --chain-id sei-chain
