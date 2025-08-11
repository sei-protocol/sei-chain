#!/bin/sh

# This runs benchmarks, by default from master branch of
# github.com/cosmos/iavl
# You can customize this by optional command line args
#
# INSTALL_USER.sh [branch] [repouser]
#
# set repouser as your username to time your fork

BRANCH=${1:-master}
REPOUSER=${2:-tendermint}

export PATH=$PATH:/usr/local/go/bin
export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin
export GOROOT=/usr/local/go
export GOPATH=$HOME/go

export CGO_CFLAGS="-I/usr/local/include"
export CGO_LDFLAGS="-L/usr/local/lib -lleveldb -lrocksdb -lstdc++ -lm -lz -lbz2 -lsnappy -llz4 -lzstd"

cd ~/
git clone https://github.com/${REPOUSER}/iavl
cd iavl
git checkout ${BRANCH}

make bench > results.txt

