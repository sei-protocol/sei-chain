#!/bin/bash
set -e

# Instructions:
#  - to run: ./authz_proposal_for_validator_ops.sh [grant/revoke]
#  - put grantee addresses in grantee.txt

if [[ "$1" == "grant" || "$1" == "revoke" ]]; then
   operation=$1
else
   echo "Invalid operation. Please pass either 'grant' or 'revoke'."
   exit 1
fi


rm /tmp/grant-*.json || true
rm /tmp/revoke-*.json || true

if [[ -z "${GRANTER}" ]]; then
    echo -n GRANTER:
    read granter 
    export GRANTER="${granter}"
    echo
    printf 'Please run "export GRANTER=%s" to avoid setting it in the future or add it to your bashrc file.'
    echo 
    echo
else
    echo "\$GRANTER is set to '$GRANTER'"
fi

if [[ -z "${CHAIN_ID}" ]]; then
    echo -n CHAIN_ID:
    read chain_id 
    export CHAIN_ID="${chain_id}"
    echo
    printf 'Please run "export CHAIN_ID=%s" to avoid setting it in the future or add it to your bashrc file.'
    echo 
    echo
else
    echo "\$CHAIN_ID is set to '$CHAIN_ID'"
fi

printf 'Grantees (reading from grantee.txt):'
echo
for GRANTEE in $(cat grantee.txt); do
    echo " - $GRANTEE"
done

if [[ $operation == "grant" ]]
then
    for GRANTEE in $(cat grantee.txt); do
        echo
        echo "generating MsgSubmitProposal, MsgVote, MsgUnjail for grantee: '$GRANTEE'"
        seid tx authz grant $GRANTEE generic --from $GRANTER -b block -y --fees 20000usei --msg-type "/cosmos.gov.v1beta1.MsgSubmitProposal" --generate-only | jq > /tmp/grant-authz-proposal-$GRANTEE.json
        seid tx authz grant $GRANTEE generic --from $GRANTER -b block -y --fees 20000usei --msg-type "/cosmos.gov.v1beta1.MsgVote" --generate-only | jq > /tmp/grant-authz-vote-$GRANTEE.json
        seid tx authz grant $GRANTEE generic --from $GRANTER -b block -y --fees 20000usei --msg-type "/cosmos.slashing.v1beta1.MsgUnjail" --generate-only | jq > /tmp/grant-authz-unjail-$GRANTEE.json

        echo
        echo "generating feegrant for grantee: '$GRANTEE'"
        seid tx feegrant grant $GRANTER $GRANTEE --allowed-messages "/cosmos.authz.v1beta1.MsgExec" --spend-limit 10sei -b block -y --fees 20000usei --from $GRANTER --generate-only | jq > /tmp/grant-feegrant-$GRANTEE.json
    done

    echo
    echo "aggregating messages for grants"
    echo
    jq -s 'reduce .[] as $item ({"body": {"messages": []}, "auth_info": .[0].auth_info, "signatures": .[0].signatures}; .body.messages += $item.body.messages)' /tmp/grant-*.json > /tmp/combined_messages.json
else
    for GRANTEE in $(cat grantee.txt); do
        echo
        echo "generating MsgSubmitProposal, MsgVote, MsgUnjail for grantee: '$GRANTEE'"
        seid tx authz revoke $GRANTEE --from $GRANTER -b block -y --fees 20000usei "/cosmos.gov.v1beta1.MsgSubmitProposal" --generate-only | jq > /tmp/revoke-authz-proposal-$GRANTEE.json
        seid tx authz revoke $GRANTEE --from $GRANTER -b block -y --fees 20000usei "/cosmos.gov.v1beta1.MsgVote" --generate-only | jq > /tmp/revoke-authz-vote-$GRANTEE.json
        seid tx authz revoke $GRANTEE --from $GRANTER -b block -y --fees 20000usei "/cosmos.slashing.v1beta1.MsgUnjail" --generate-only | jq > /tmp/revoke-authz-unjail-$GRANTEE.json

        echo
        echo "generating feegrant for grantee: '$GRANTEE'"
        seid tx feegrant revoke $GRANTER $GRANTEE --fees 20000usei --from $GRANTER -b block -y --generate-only | jq > /tmp/revoke-feegrant-$GRANTEE.json
    done

    echo
    echo "aggregating messages for revokes"
    echo
    jq -s 'reduce .[] as $item ({"body": {"messages": []}, "auth_info": .[0].auth_info, "signatures": .[0].signatures}; .body.messages += $item.body.messages)' /tmp/revoke-*.json > /tmp/combined_messages.json
fi

echo "combined messages have been stored in /tmp/combined_messages.json, please sign and broadcast them manually"
echo 
echo "To sign messages: 'seid tx sign /tmp/combined_messages.json --from \$GRANTER --chain-id \$CHAIN_ID | jq > signed_tx.json'"
echo
echo "To broadcast signed tx: 'seid tx broadcast signed_tx.json --chain-id \$CHAIN_ID -b block'"
