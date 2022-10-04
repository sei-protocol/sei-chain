seid tx gov submit-proposal update-resource-dependency-mapping ../brando/acl-gov.json --from brando  --fees=10000000usei --gas=5000000  --chain-id sei-chain  -b block -y
seid tx gov deposit 1 10000000usei --chain-id sei-chain --from brando --fees=10000000usei --gas=5000000 -b block -y
seid tx gov vote 1 yes --chain-id sei-chain --from brando -b block -y --fees=10000000usei
