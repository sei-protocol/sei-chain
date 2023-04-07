#!/bin/bash

INITIAL_HALT_HEIGHT=$1
SNAPSHOT_INTERVAL=$2
CHAIN_ID=$3

systemctl stop seid

sed -i -e 's/pruning = .*/pruning = "custom"/' /root/.sei/config/app.toml
sed -i -e 's/pruning-keep-recent = .*/pruning-keep-recent = "1"/' /root/.sei/config/app.toml
sed -i -e 's/pruning-keep-every = .*/pruning-keep-every = "0"/' /root/.sei/config/app.toml
sed -i -e 's/pruning-interval = .*/pruning-interval = "1"/' /root/.sei/config/app.toml

mkdir -p /root/snapshots

HALT_HEIGHT=$INITIAL_HALT_HEIGHT
while true
do
    sed -i -e 's/halt-height = .*/halt-height = '$HALT_HEIGHT'/' /root/.sei/config/app.toml
    /root/go/bin/seid start --trace --chain-id $CHAIN_ID
    start_time=$(date +%s)
    cd /root/snapshots
    rm -f LATEST_HEIGHT
    touch LATEST_HEIGHT
    echo $HALT_HEIGHT >> LATEST_HEIGHT
	mkdir /root/snapshots/snapshot_$HALT_HEIGHT
    cp -r /root/.sei/data/application.db/* /root/snapshots/snapshot_$HALT_HEIGHT/
    cd /root/snapshots/snapshot_$HALT_HEIGHT
    touch METADATA
    for FILE in *;
    do
        echo $FILE >> METADATA;
    done
    end_time=$(date +%s)
    elapsed=$(( end_time - start_time ))
	echo "Backed up snapshot for height "$HALT_HEIGHT" which took "$elapsed" seconds"
    HALT_HEIGHT=$(expr $HALT_HEIGHT + $SNAPSHOT_INTERVAL)
    cd $HOME
done
