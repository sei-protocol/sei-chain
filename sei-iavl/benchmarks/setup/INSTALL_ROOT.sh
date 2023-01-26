#!/bin/sh

export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get -y upgrade
apt-get -y install screen wget git build-essential libsnappy-dev libgflags-dev zlib1g-dev libbz2-dev liblz4-dev libzstd-dev

# Installing leveldb from source
cd ~/
git clone https://github.com/google/leveldb
cd leveldb
git checkout v1.7
make -j2
cp --preserve=links libleveldb.* /usr/local/lib
cp -r include/leveldb /usr/local/include/
ldconfig

# installing rocksdb from source
cd ~/
git clone https://github.com/facebook/rocksdb
cd rocksdb
git checkout v6.15.5
make -j4 install-shared
ldconfig

# install go
cd ~/
mkdir go
wget https://go.dev/dl/go1.17.6.linux-amd64.tar.gz
tar -C /usr/local -xzf go1.17.6.linux-amd64.tar.gz

