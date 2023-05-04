#!/bin/bash

jq '.validators = []' ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
cd build/generated/gentx
IDX=0
for FILE in *
do
    jq '.validators['$IDX'] |= .+ {}' ~/.sei/config/tmp_genesis.json > ~/.sei/config/tmp_genesis_step_1.json && rm ~/.sei/config/tmp_genesis.json
    KEY=$(jq '.body.messages[0].pubkey.key' $FILE -c)
    DELEGATION=$(jq -r '.body.messages[0].value.amount' $FILE)
    POWER=$(($DELEGATION / 1000000))
    jq '.validators['$IDX'] += {"power":"'$POWER'"}' ~/.sei/config/tmp_genesis_step_1.json > ~/.sei/config/tmp_genesis_step_2.json && rm ~/.sei/config/tmp_genesis_step_1.json
    jq '.validators['$IDX'] += {"pub_key":{"type":"tendermint/PubKeyEd25519","value":'$KEY'}}' ~/.sei/config/tmp_genesis_step_2.json > ~/.sei/config/tmp_genesis_step_3.json && rm ~/.sei/config/tmp_genesis_step_2.json
    mv ~/.sei/config/tmp_genesis_step_3.json ~/.sei/config/tmp_genesis.json
    IDX=$(($IDX+1))
done

mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
