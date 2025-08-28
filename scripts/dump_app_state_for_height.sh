#!/bin/bash

if ! command -v zip &> /dev/null
then
    echo "zip could not be found"
    exit
fi

if [ -z "$1" ]
then
  echo "Must provide a height as the first parameter"
  exit
fi
HEIGHT=$1

# install iaviewer
if ! command -v $HOME/go/bin/iaviewer &> /dev/null
then
    cd $HOME
    rm -rf iavl
    git clone https://github.com/cosmos/iavl.git
    cd iavl
    git checkout v0.17.3
    make install
fi

# dump state for height
cd $HOME
sudo rm -r state_$HEIGHT
mkdir state_$HEIGHT
for key in wasm accesscontrol oracle epoch mint acc bank crisis feegrant staking distribution slashing gov params ibc upgrade evidence transfer tokenfactory
do
    $HOME/go/bin/iaviewer data $HOME/.sei/data/application.db "s/k:"$key"/" $HEIGHT > $HOME/state_$HEIGHT/$key.data
    $HOME/go/bin/iaviewer shape $HOME/.sei/data/application.db "s/k:"$key"/" $HEIGHT > $HOME/state_$HEIGHT/$key.shape
done

# zip
zip -r $HOME/state_$HEIGHT.zip $HOME/state_$HEIGHT
