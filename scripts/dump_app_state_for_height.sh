#!/bin/bash

if ! command -v zip &> /dev/null
then
    echo "zip could not be found"
    exit
fi

HEIGHT=$1

# install iaviewer
if ! command -v ~/go/bin/iaviewer &> /dev/null
then
    cd ~
    sudo rm -r iavl
    git clone https://github.com/cosmos/iavl.git
    cd iavl
    git checkout v0.17.3
    make install
fi

# dump state for height
cd ~
sudo rm -r state_$HEIGHT
mkdir state_$HEIGHT
for key in dex wasm oracle epoch mint
do
    ~/go/bin/iaviewer data ~/.sei/data/application.db "s/k:"$key"/" $HEIGHT > ~/state_$HEIGHT/$key.data
    ~/go/bin/iaviewer shape ~/.sei/data/application.db "s/k:"$key"/" $HEIGHT > ~/state_$HEIGHT/$key.shape
done

# zip
zip -r ~/state_$HEIGHT.zip ~/state_$HEIGHT
