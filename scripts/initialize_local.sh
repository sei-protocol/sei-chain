# min go compiler version >=1.18.2
# gvm use go1.18.2

#########################################################
## How to submitting a proposal after chain is running ##
#########################################################
# To submit a new proposal:
# ./build/seid tx gov submit-proposal param-change proposal.json --from alice --chain-id sei --fees 2000usei -b block -y
# To vote a proposal:
# ./build/seid tx gov vote <proposal_id> yes --from alice --chain-id sei --fees 2000usei -b block -y
# To query a proposal status:
# ./build/seid q gov proposal <proposal_id>
# To query a transaction status:
# ./build/seid q tx --type=hash <tx_hash>
#########################################################

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
./build/seid keys add $test_account_name --keyring-backend test
./build/seid add-genesis-account $(./build/seid keys show $test_account_name -a) 100000000000000000000usei
# generate genesis tx
./build/seid gentx $test_account_name 70000000000000000000usei --chain-id sei
sed -i'' -e 's/mode = "full"/mode = "validator"/g' $HOME/.sei/config/config.toml
sed -i'' -e 's/indexer = \["null"\]/indexer = \["kv"\]/g' $HOME/.sei/config/config.toml
KEY=$(jq '.pub_key' ~/.sei/config/priv_validator_key.json -c)
jq '.validators = [{}]' ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
jq '.validators[0] += {"power":"70000000000000"}' ~/.sei/config/tmp_genesis.json > ~/.sei/config/tmp_genesis_2.json
jq '.validators[0] += {"pub_key":'$KEY'}' ~/.sei/config/tmp_genesis_2.json > ~/.sei/config/tmp_genesis_3.json
mv ~/.sei/config/tmp_genesis_3.json ~/.sei/config/genesis.json && rm ~/.sei/config/tmp_genesis.json && rm ~/.sei/config/tmp_genesis_2.json
./build/seid collect-gentxs
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["deposit_params"]["max_deposit_period"]="300s"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["voting_params"]["voting_period"]="120s"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["distribution"]["params"]["community_tax"]="0.000000000000000000"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
./build/seid start --chain-id sei-chain
