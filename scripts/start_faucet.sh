#!/bin/bash

echo -n OS Password:
read -s password
echo

curl "https://get.starport.network/faucet!" > /tmp/faucet_install.sh
echo $password | sudo -S bash /tmp/faucet_install.sh
faucet --cli-name ./build/newd --denoms usei
