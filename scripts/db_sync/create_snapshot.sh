#!/bin/bash


display_usage() {
  echo "This script create snapshot for db sync, recommend to run it as a systemd service"
  echo "Usage: create_snapshot.sh [interval] [snapshot_dir]"
}

SNAPSHOT_INTERVAL=$1
SNAPSHOT_DIRECTORY=$2

if [[ ( $1 == "--help") ||  $1 == "-h" ]];
then
  display_usage
  exit 0
fi

if [[ $# -ne 2 ]];
then
  display_usage
  exit 1
fi

if [[ $1 -le 0 ]];
then
  echo "Snapshot interval $SNAPSHOT_INTERVAL is invalid, needs to be a positive number"
  exit 1
fi

#########################################
# Override pruning settings for db sync #
#########################################
sed -i -e 's/pruning = .*/pruning = "custom"/' /root/.sei/config/app.toml
sed -i -e 's/pruning-keep-recent = .*/pruning-keep-recent = "1"/' /root/.sei/config/app.toml
sed -i -e 's/pruning-keep-every = .*/pruning-keep-every = "0"/' /root/.sei/config/app.toml
sed -i -e 's/pruning-interval = .*/pruning-interval = "1"/' /root/.sei/config/app.toml

##############################
# Wait for node to catch up ##
##############################
while [[ "$(seid status |jq -r .SyncInfo.latest_block_height)" -le 0 ]]
do
  echo "Waiting for seid state sync to complete"
  sleep 5
done

while [[ $(( $(seid status | jq -r .SyncInfo.max_peer_block_height) - $(seid status | jq -r .SyncInfo.latest_block_height) )) -ge 50 ]]
do
  echo "Waiting for node to catch up"
  sleep 5
done

############################
# Set initial halt height ##
############################
CURRENT_HEIGHT=$(seid status |jq -r .SyncInfo.latest_block_height)
if [[ $CURRENT_HEIGHT -le 0 ]]
then
  echo "Failed to get latest block height"
  exit 1
fi

systemctl stop seid
INITIAL_HALT_HEIGHT=$((CURRENT_HEIGHT + SNAPSHOT_INTERVAL))
mkdir -p "$SNAPSHOT_DIRECTORY"
HALT_HEIGHT=$INITIAL_HALT_HEIGHT
sed -i -e 's/halt-height = .*/halt-height = '$HALT_HEIGHT'/' /root/.sei/config/app.toml
systemctl restart seid


#######################################
# Main Loop to keep creating snapshot #
#######################################
while true
do
  #TODO: Wait until node height reaches the next halt height, e.g. persist latest block height via a file before it hits halt height
  sleep 15
  while seid status > /dev/null 2>&1 ;
  do
    echo "Waiting for the node to hit next halt height"
    sleep 15
  done
  systemctl stop seid
  seid tendermint snapshot $HALT_HEIGHT
  HALT_HEIGHT=$(expr $HALT_HEIGHT + $SNAPSHOT_INTERVAL)
  sed -i -e 's/halt-height = .*/halt-height = '$HALT_HEIGHT'/' /root/.sei/config/app.toml
  systemctl daemon-reload
  systemctl restart seid
done
