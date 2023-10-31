#!/bin/bash
set -e

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

echo

for GRANTEE in $(cat grantee.txt); do
    echo
    echo "generating MsgSubmitProposal, MsgVote, MsgUnjail for grantee: '$GRANTEE'"
    seid tx authz grant $GRANTEE generic --from admin -b block -y --fees 20000usei --msg-type "/cosmos.gov.v1beta1.MsgSubmitProposal,/cosmos.gov.v1beta1.MsgVote,/cosmos.slashing.v1beta1.MsgUnjail" --generate-only | jq > /tmp/grant-authz-$GRANTEE.json

    echo
    echo "generating feegrant for grantee: '$GRANTEE'"
    seid tx feegrant grant admin $GRANTEE --allowed-messages "/cosmos.authz.v1beta1.MsgExec" --spend-limit 10sei -b block -y --fees 20000usei --from $GRANTER --generate-only | jq > /tmp/grant-feegrant-$GRANTEE.json
done

echo
echo "aggregating messages for grants"
echo
jq -s 'reduce .[] as $item ({"body": {"messages": []}, "auth_info": .[0].auth_info, "signatures": .[0].signatures}; .body.messages += $item.body.messages)' /tmp/grant-*.json > /tmp/combined_messages.json

echo
echo "signing combined messages"
echo
printf "12345678\n" | seid tx sign /tmp/combined_messages.json --from $GRANTER --chain-id $CHAIN_ID | jq > signed_tx.json

echo
echo "broadcasting signed tx"
echo
seid tx broadcast signed_tx.json --chain-id $CHAIN_ID -b block -y; done

# remove grant* files so they don't get used in a separate run with different granters
# for GRANTEE in $(cat grantee.txt); do
#     rm /tmp/grant-authz-$GRANTEE.json
#     rm /tmp/grant-feegrant-$GRANTEE.json
# done


