#!/bin/bash
#v0.3alpha
#snipeTR 2022 dscrd:snipeTR#8374
#karboran 2022 dscrd:karboran#2719
#Apache License Version 2.0
#################################################################################################################
#                                                                                                               #
#  This script is for cosmovisor and sei-chain node setup.
#                                                                                                               #
#  cosmovisor is an automatic node update and tracking system for the cosmos ecosystem.
#                                                                                                               #
#  Here's what the script does:
#                                                                                                               #
#  It installs the 1.0.2beta version of the seid application for sei-chain-testnet-2.
#                                                                                                               #
#  Makes various configuration changes of 1.0.2beta version of seid application for sei-chain-testnet-2.
#                                                                                                               #
#  It installs and builds the cosmovisor application.
#                                                                                                               #
#  Creates the folder structure required for the cosmovisor application.
#                                                                                                               #
#  Installs seid 1.0.2beta version to the cosmovisor application.
#                                                                                                               #
#  It downloads seid 1.0.3beta version, builds it and installs it on the cosmovisor application.
#                                                                                                               #
#  seid downloads the 1.0.4beta version, builds it and installs it on the cosmovisor application.
#                                                                                                               #
#  seid downloads the 1.0.5beta version, builds it and installs it on the cosmovisor application.
#                                                                                                               #
#  seid downloads the 1.0.6beta version, builds and installs it on the cosmovisor application.
#                                                                                                               #
#################################################################################################################

#~/.sei/cosmovisor
#       ├─ current -> genesis or upgrades/<name>
#      	├── genesis
#      	│    └── bin
#      	│      └── seid
#      	├── upgrades
#      	│   └── 1.0.3beta
#      	│       ├── bin
#      	│       │   └── seid
#      	│       └── upgrade-info.json
#      	├── upgrades
#      	│   └── 1.0.4beta
#      	│       ├── bin
#      	│       │   └── seid
#      	│       └── upgrade-info.json
#      	├── upgrades
#      	│   └── 1.0.5beta%20upgrade
#      	│       ├── bin
#      	│       │   └── seid
#      	│       └── upgrade-info.json
#      	└── upgrades
#      	    └── 1.0.6beta
#      	        ├── bin
#      	        │   └── seid
#      	        └── upgrade-info.json

if [ ! "$NODENAME" ]; then
        read -p -r "Node name:" NODENAME
        echo 'export NODENAME'="$NODENAME" >> "$HOME"/.bash_profile
fi
if [ ! "$WALLET" ]; then
echo "export WALLET=wallet" >> "$HOME"/.bash_profile
fi
if [ ! "$CHAIN_ID" ]; then
echo "export CHAIN_ID=sei-testnet-2" >> "$HOME"/.bash_profile
fi
if [ ! "$DAEMON_RESTART_AFTER_UPGRADE" ]; then
echo "export DAEMON_RESTART_AFTER_UPGRADE=true" >> "$HOME"/.bash_profile
fi
if [ ! "$DAEMON_NAME" ]; then
echo "export DAEMON_NAME=seid" >> "$HOME"/.bash_profile
fi
if [ ! "$DAEMON_HOME" ]; then
echo "export DAEMON_HOME=$HOME/.sei" >> "$HOME"/.bash_profile
fi
if [ ! "$SEIDVER" ]; then
echo "export SEIDVER=1.0.2beta" >> "$HOME"/.bash_profile
fi
if [ ! "$VISORVER" ]; then
echo "export VISORVER=v1.1.0" >> "$HOME"/.bash_profile
fi
source "$HOME"/.bash_profile

echo '==================VARIABLE CHECK================='
echo NODENAME:"$NODENAME"
echo WALLET:"$WALLET"
echo CHAIN_ID:"$CHAIN_ID"
echo DAEMON_RESTART_AFTER_UPGRADE:"$DAEMON_RESTART_AFTER_UPGRADE"
echo DAEMON_NAME:"$DAEMON_NAME"
echo DAEMON_HOME:"$DAEMON_HOME"
echo SEIDVER:"$SEIDVER"
echo VISORVER:"$VISORVER"
echo '================================================='
read -r -s -n 1 -p "Are the above values correct? If true, press a key. If false, exit with ctrl+c"

sudo systemctl stop seid
sudo systemctl disable seid
sudo systemctl disable seid.service
sudo systemctl daemon-reload
echo -e "\e[1m\e[32m old seid services disable and stop\e[0m" && sleep 1

sleep 1
echo -e "\e[1m\e[32m1. Update and Upgrade check... \e[0m" && sleep 1
# update
sudo apt update && sudo apt upgrade -y

echo -e "\e[1m\e[32m2. installing tools and libs... \e[0m" && sleep 1
# packages
sudo apt install curl tar wget clang pkg-config libssl-dev jq build-essential bsdmainutils git make ncdu gcc git jq chrony tmux net-tools liblz4-tool -y

sleep 1
cd $HOME || { echo "$HOME" dizinine girilemiyor"; sleep 10; exit 13;}
wget -O go1.18.1.linux-amd64.tar.gz https://golang.org/dl/go1.18.1.linux-amd64.tar.gz
rm -rf /usr/local/go && tar -C /usr/local -xzf go1.18.1.linux-amd64.tar.gz && rm go1.18.1.linux-amd64.tar.gz
echo 'export GOROOT=/usr/local/go' >> "$HOME"/.bash_profile
echo 'export GOPATH=$HOME/go' >> "$HOME"/.bash_profile
echo 'export GO111MODULE=on' >> "$HOME"/.bash_profile
echo 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin' >> "$HOME"/.bash_profile && . "$HOME"/.bash_profile
go version
source "$HOME"/.bash_profile

echo -e "\e[1m\e[32m3. sei-chain library download and install... \e[0m"

# download binary
cd $HOME || { echo "$HOME" dizinine girilemiyor"; sleep 10; exit 13;}
echo git clone --depth 1 --branch "$SEIDVER" https://github.com/sei-protocol/sei-chain.git
git clone --depth 1 --branch "$SEIDVER" https://github.com/sei-protocol/sei-chain.git
cd sei-chain && make install
go build -o build/seid ./cmd/seid

#unnecessary maybe necessary
#chmod +x ./build/seid && sudo cp ./build/seid /usr/local/bin/seid

sleep 1

#impornant
cp "$HOME"/go/bin/seid /usr/local/bin/

mv "$HOME"/.sei-chain "$HOME"/.sei

#if not exist /usr/local/bin/seid write ERROR
#if exist /usr/local/bin/seid and version NOT not match write ERROR
if [ ! -f "/usr/local/bin/seid" ]; then
    echo "/usr/local/bin/seid FILE NOT EXIST"
    read -r -s -n 1 -p "Press any key to EXIT . . ."
    exit 13
else
  SEIDBUILDVER=$(/usr/local/bin/seid version)
  if [ "$SEIDBUILDVER" == "$SEIDVER" ]; then
  	echo /usr/local/bin/seid version "$SEIDVER"
  else
    	echo -e "\e[1m\e[31m2. Error version not match $SEIDVER \e[0m"
    	echo -e "\e[1m\e[31m2. please check /usr/local/bin/seid version file \e[0m"
    	read -r -s -n 1 -p "Press any key to EXIT . . ."
  	exit 13
  fi
fi

# config
seid config chain-id "$CHAIN_ID"
seid config keyring-backend file

# init
seid init "$NODENAME" --chain-id "$CHAIN_ID"

# download genesis and addrbook
wget -qO "$HOME"/.sei/config/genesis.json "https://raw.githubusercontent.com/sei-protocol/testnet/main/sei-testnet-2/genesis.json"
wget -qO "$HOME"/.sei/config/addrbook.json "https://raw.githubusercontent.com/sei-protocol/testnet/main/sei-testnet-2/addrbook.json"

#Tuning config.toml
CONFIG_PATH="$HOME/.sei/config/config.toml"
if [[ "$OSTYPE" == "linux-gnu"* ]]; then
  sed -i 's/max_num_inbound_peers =.*/max_num_inbound_peers = 150/g' "$CONFIG_PATH"
  sed -i 's/max_num_outbound_peers =.*/max_num_outbound_peers = 150/g' "$CONFIG_PATH"
  sed -i 's/max_packet_msg_payload_size =.*/max_packet_msg_payload_size = 10240/g' "$CONFIG_PATH"
  sed -i 's/send_rate =.*/send_rate = 20480000/g' "$CONFIG_PATH"
  sed -i 's/recv_rate =.*/recv_rate = 20480000/g' "$CONFIG_PATH"
  sed -i 's/max_txs_bytes =.*/max_txs_bytes = 10737418240/g' "$CONFIG_PATH"
  sed -i 's/^size =.*/size = 5000/g' "$CONFIG_PATH"
  sed -i 's/max_tx_bytes =.*/max_tx_bytes = 2048576/g' "$CONFIG_PATH"
  sed -i 's/timeout_prevote =.*/timeout_prevote = "100ms"/g' "$CONFIG_PATH"
  sed -i 's/timeout_precommit =.*/timeout_precommit = "100ms"/g' "$CONFIG_PATH"
  sed -i 's/timeout_commit =.*/timeout_commit = "100ms"/g' "$CONFIG_PATH"
  sed -i 's/skip_timeout_commit =.*/skip_timeout_commit = true/g' "$CONFIG_PATH"
else
  printf "Platform not supported, please ensure that the following values are set in your config.toml:\n"
  printf "###          Mempool Configuration Option          ###\n"
  printf "\t size = 5000\n"
  printf "\t max_txs_bytes = 10737418240\n"
  printf "\t max_tx_bytes = 2048576\n"
  printf "###           P2P Configuration Options             ###\n"
  printf "\t max_num_inbound_peers = 150\n"
  printf "\t max_num_outbound_peers = 150\n"
  printf "\t max_packet_msg_payload_size = 10240\n"
  printf "\t send_rate = 20480000\n"
  printf "\t recv_rate = 20480000\n"
  printf "###         Consensus Configuration Options         ###\n"
  printf "\t timeout_prevote = \"100ms\"\n"
  printf "\t timeout_precommit = \"100ms\"\n"
  printf "\t timeout_commit = \"100ms\"\n"
  printf "\t skip_timeout_commit = true\n"
  read -r -s -n 1 -p "Press any key to EXIT . . ."
  exit 13
fi

# set minimum gas price
sed -i -e "s/^minimum-gas-prices *=.*/minimum-gas-prices = \"0usei\"/" "$HOME"/.sei/config/app.toml

# Type the peers you want to add here and delete the two comment characters below.
# PEERS=""
# sed -i.bak -e "s/^persistent_peers *=.*/persistent_peers = \"$PEERS\"/" "$HOME"/.sei/config/config.toml

# config pruning
pruning="custom"
pruning_keep_recent="100"
pruning_keep_every="0"
pruning_interval="10"

sed -i -e "s/^pruning *=.*/pruning = \"$pruning\"/" "$HOME"/.sei/config/app.toml
sed -i -e "s/^pruning-keep-recent *=.*/pruning-keep-recent = \"$pruning_keep_recent\"/" "$HOME"/.sei/config/app.toml
sed -i -e "s/^pruning-keep-every *=.*/pruning-keep-every = \"$pruning_keep_every\"/" "$HOME"/.sei/config/app.toml
sed -i -e "s/^pruning-interval *=.*/pruning-interval = \"$pruning_interval\"/" "$HOME"/.sei/config/app.toml


#When you are going to run multiple NODEs on the same server, please change the ports using the commands below to avoid port conflict.
#Change port 37 
#------------------------------
#26658 to 36378
port_a=36378

#26657 to 36377
port_b=36377

#6060 to 6371 
port_c=6371

#26656 to 36376
port_d=36376

#26660 to 36370
port_e=36370

#9090 to 9370
port_f=9370

#9091 to 9371
port_g=9371
#------------------------------
#To make the above changes, delete the comment character of the 5 lines below.
#sed -i.bak -e "s%^proxy_app = \"tcp://127.0.0.1:26658\"%proxy_app = \"tcp://127.0.0.1:$port_a\"%; s%^laddr = \"tcp://127.0.0.1:26657\"%laddr = \"tcp://127.0.0.1:$port_b\"%; s%^pprof_laddr = \"localhost:6060\"%pprof_laddr = \"localhost:$port_c\"%; s%^laddr = \"tcp://0.0.0.0:26656\"%laddr = \"tcp://0.0.0.0:$port_d\"%; s%^prometheus_listen_addr = \":26660\"%prometheus_listen_addr = \":$port_e\"%" $HOME/.sei/config/config.toml
#sed -i.bak -e "s%^address = \"0.0.0.0:9090\"%address = \"0.0.0.0:$port_f\"%; s%^address = \"0.0.0.0:9091\"%address = \"0.0.0.0:$port_g\"%" $HOME/.sei/config/app.toml
#sed -i.bak -e "s%^node = \"tcp://localhost:26657\"%node = \"tcp://localhost:$port_b\"%" $HOME/.sei/config/client.toml
#external_address=$(wget -qO- eth0.me)
#sed -i.bak -e "s/^external_address *=.*/external_address = \"$external_address:$port_d\"/" $HOME/.sei/config/config.toml

sleep 1
# reset
seid unsafe-reset-all

echo -e "\e[1m\e[32m4. Make and start SERVICES... \e[0m" && sleep 1
# create service
tee "$HOME"/seid.service > /dev/null <<EOF
[Unit]
Description=seid
After=network.target
[Service]
Type=simple
User=$USER
ExecStart=$(which seid) start
Restart=on-failure
RestartSec=10
LimitNOFILE=65535
[Install]
WantedBy=multi-user.target
EOF

sudo mv "$HOME"/seid.service /etc/systemd/system/

# start service
sudo systemctl daemon-reload
#              	disable start and enable
#sudo systemctl enable seid
#sudo systemctl restart seid

#cosmovisor build
cd $HOME || { echo "$HOME" dizinine girilemiyor"; sleep 10; exit 13;}
rm -rf ./cosmos-sdk
#git clone git@github.com:cosmos/cosmos-sdk
git clone --depth 1 --branch main https://github.com/cosmos/cosmos-sdk

#$HOME/cosmos-sdk/cosmovisor/cmd/cosmovisor/version.go file, line 21 [Version = "1.1.0"] >>> change >>> [Version = "v1.1.0"]
#sed -i -e "s/Version *=.*/Version = \"v1.1.0\"/" $HOME/cosmos-sdk/cosmovisor/cmd/cosmovisor/version.go

cd cosmos-sdk && echo "cosmos-sdk" dizinine girilemiyor && sleep 10 && exit 13
#git checkout cosmovisor/$VISORVER
make cosmovisor

#if commovisor exist $HOME/cosmos-sdk/cosmovisor/ and version 1.1.0 copy usr/local/bin
if [ ! -f "$HOME/cosmos-sdk/cosmovisor/cosmovisor" ]; then
    echo "cosmovisor not build. ERROR ERROR"
    read -r -s -n 1 -p "Press any key to EXIT . . ."
  	exit 13
#          cosmovisor version check unnessesery
#	else
#  chmod +x $HOME/cosmos-sdk/cosmovisor/cosmovisor
#  VISORBUILDVER=$($HOME/cosmos-sdk/cosmovisor/cosmovisor version)
#  
#  #reformat variable (v1.1.0 for :21:6)
#  #********************************************************************************************
#  #(Cosmovisor Version: v1.1.0 9:56PM ERR Can't ru...)<<<---- variable string(please paste)  *
#  #(                    ^    ^                    ...)                    *
#  #(1234567890123456789012345678901234567890123456...)<<<---- char counting          *
#  #(                    ^    ^                    ...)                    *
#  #(               21-->123456<--6                ...)<<<---- number meaning ${variable:21:6}	*
#  #********************************************************************************************
#  #reformat variable (v1.1.0 for :21:6)
#  
#  VISORBUILDVER=${VISORBUILDVER:21:6}
#  	if [ "$VISORBUILDVER" == "$VISORVER" ]; then
#      	cp $HOME/cosmos-sdk/cosmovisor/cosmovisor /usr/local/bin/
#    else
#      	echo -e "\e[1m\e[31m Error version not match $VISORVER \e[0m"
#      	echo -e "\e[1m\e[31m please check $HOME/cosmos-sdk/cosmovisor/cosmovisor version file \e[0m"
#      	read -s -n 1 -p "Press any key to EXIT . . ."
#    	exit 13
#    fi
fi

# Checkout the binary for 1.0.2beta on cosmovisor
#################################################
cp "$HOME"/cosmos-sdk/cosmovisor/cosmovisor /usr/local/bin/
#Checkout the binary for genesis 1.0.2beta on cosmovisor
cd $HOME || { echo "$HOME" dizinine girilemiyor"; sleep 10; exit 13;}
mkdir -p "$DAEMON_HOME"/cosmovisor/genesis/bin
cp "$HOME"/go/bin/seid "$DAEMON_HOME"/cosmovisor/genesis/bin
#if not exist $DAEMON_HOME/cosmovisor/genesis/bin/seid write ERROR
if [ -f "$DAEMON_HOME/cosmovisor/genesis/bin/seid" ]; then
     echo "$DAEMON_HOME"/cosmovisor/genesis/bin/seid file copy successful
else
      	echo -e "\e[1m\e[31m ERROR $HOME/go/bin/seid not copy $DAEMON_HOME/cosmovisor/genesis/bin \e[0m"
      	echo -e "\e[1m\e[31m please check $HOME/go/bin/seid file \e[0m"
      	read -r -s -n 1 -p "Press any key to EXIT . . ."
    	exit 13
fi
#################################################

# Checkout the binary for 1.0.3beta on cosmovisor
#################################################
#set version
SEIDVER=1.0.3beta
cd $HOME || { echo "$HOME" dizinine girilemiyor"; sleep 10; exit 13;}
#remove old sei-chain directory
rm -rf sei-chain
#clone sei-chain
git clone --depth 1 https://github.com/sei-protocol/sei-chain.git
cd "sei-chai" && echo "sei-chai" dizinine girilemiyor && sleep 10 && exit 13
git fetch --tags -f
git checkout $SEIDVER
# Build the new version
make install
go build -o build/seid ./cmd/seid
# Checkout the binary for 1.0.xbeta
# if not right version exit script.
cd $HOME || { echo "$HOME" dizinine girilemiyor"; sleep 10; exit 13;}
#create cosmovisor upgrade directory
mkdir -p "$DAEMON_HOME"/cosmovisor/upgrades/$SEIDVER/bin
#copy new seid version correct upgrade cosmovisor directory
cp "$HOME"/go/bin/seid "$DAEMON_HOME"/cosmovisor/upgrades/"$SEIDVER"/bin
if [ ! -f "$DAEMON_HOME/cosmovisor/upgrades/$SEIDVER/bin/seid" ]; then
    echo "$DAEMON_HOME/cosmovisor/upgrades/$SEIDVER/bin/seid FILE NOT EXIST"
    read -r -s -n 1 -p "Press any key to EXIT . . ."
    exit 13
else
  SEIDBUILDVER=$("$DAEMON_HOME"/cosmovisor/upgrades/"$SEIDVER"/bin/seid version)
  if [ "$SEIDBUILDVER" == "$SEIDVER" ]; then
  	echo "$DAEMON_HOME"/cosmovisor/upgrades/$SEIDVER/bin/seid version $SEIDVER
  else
    	echo -e "\e[1m\e[31m2. Error version not match $SEIDVER \e[0m"
    	echo -e "\e[1m\e[31m2. please check $DAEMON_HOME/cosmovisor/upgrades/$SEIDVER/bin version file \e[0m"
    	read -r -s -n 1 -p "Press any key to EXIT . . ."
  	exit 13
  fi
fi
#################################################


# Checkout the binary for 1.0.4beta
#################################################
SEIDVER=1.0.4beta
cd $HOME || { echo "$HOME" dizinine girilemiyor"; sleep 10; exit 13;}
#remove old sei-chain directory
rm -rf sei-chain
#clone sei-chain
git clone --depth 1 https://github.com/sei-protocol/sei-chain.git
cd "sei-chai" && echo "sei-chai" dizinine girilemiyor && sleep 10 && exit 13
git fetch --tags -f
git checkout $SEIDVER
# Build the new version
make install
go build -o build/seid ./cmd/seid
# Checkout the binary for 1.0.xbeta
# if not right version exit script.
cd $HOME || { echo "$HOME" dizinine girilemiyor"; sleep 10; exit 13;}
#create cosmovisor upgrade directory
mkdir -p "$DAEMON_HOME"/cosmovisor/upgrades/$SEIDVER/bin
#copy new seid version correct upgrade cosmovisor directory
cp "$HOME"/go/bin/seid "$DAEMON_HOME"/cosmovisor/upgrades/"$SEIDVER"/bin
if [ ! -f "$DAEMON_HOME/cosmovisor/upgrades/$SEIDVER/bin/seid" ]; then
    echo "$DAEMON_HOME/cosmovisor/upgrades/$SEIDVER/bin/seid FILE NOT EXIST"
    read -r -s -n 1 -p "Press any key to EXIT . . ."
    exit 13
else
  SEIDBUILDVER=$("$DAEMON_HOME"/cosmovisor/upgrades/"$SEIDVER"/bin/seid version)
  if [ "$SEIDBUILDVER" == "$SEIDVER" ]; then
  	echo "$DAEMON_HOME"/cosmovisor/upgrades/"$SEIDVER"/bin/seid version "$SEIDVER"
  else
    	echo -e "\e[1m\e[31m2. Error version not match $SEIDVER \e[0m"
    	echo -e "\e[1m\e[31m2. please check $DAEMON_HOME/cosmovisor/upgrades/$SEIDVER/bin version file \e[0m"
    	read -r -s -n 1 -p "Press any key to EXIT . . ."
  	exit 13
  fi
fi
#################################################


# Checkout the binary for 1.0.5beta
#################################################
SEIDVER=1.0.5beta
cd $HOME || { echo "$HOME" dizinine girilemiyor"; sleep 10; exit 13;}
#remove old sei-chain directory
rm -rf sei-chain
#clone sei-chain
git clone --depth 1 https://github.com/sei-protocol/sei-chain.git
cd "sei-chain" && echo "sei-chain" dizinine girilemiyor && sleep 10 && exit 13
git fetch --tags -f
git checkout $SEIDVER
# Build the new version
make install
go build -o build/seid ./cmd/seid
# Checkout the binary for 1.0.xbeta
# if not right version exit script.
cd $HOME || { echo "$HOME" dizinine girilemiyor"; sleep 10; exit 13;}
#create cosmovisor upgrade directory
mkdir -p "$DAEMON_HOME"/cosmovisor/upgrades/"$SEIDVER"%20upgrade/bin
#copy new seid version correct upgrade cosmovisor directory
cp "$HOME"/go/bin/seid "$DAEMON_HOME"/cosmovisor/upgrades/"$SEIDVER"%20upgrade/bin
if [ ! -f "$DAEMON_HOME/cosmovisor/upgrades/$SEIDVER%20upgrade/bin/seid" ]; then
    echo "$DAEMON_HOME/cosmovisor/upgrades/$SEIDVER%20upgrade/bin/seid FILE NOT EXIST"
    read -r -s -n 1 -p "Press any key to EXIT . . ."
    exit 13
else
  SEIDBUILDVER=$("$DAEMON_HOME"/cosmovisor/upgrades/"$SEIDVER"%20upgrade/bin/seid version)
  if [ "$SEIDBUILDVER" == "$SEIDVER" ]; then
  	echo "$DAEMON_HOME"/cosmovisor/upgrades/"$SEIDVER"%20upgrade/bin/seid version $SEIDVER
  else
    	echo -e "\e[1m\e[31m2. Error version not match $SEIDVER \e[0m"
    	echo -e "\e[1m\e[31m2. please check $DAEMON_HOME/cosmovisor/upgrades/$SEIDVER%20upgrades/bin version file \e[0m"
    	read -r -s -n 1 -p "Press any key to EXIT . . ."
  	exit 13
  fi
fi
#possible seid1.0.5beta or cosmovisor bug and create alternative folder
mkdir -p "$DAEMON_HOME"/cosmovisor/upgrades/"$SEIDVER"/bin
cp "$DAEMON_HOME"/cosmovisor/upgrades/"$SEIDVER"%20upgrade/bin/seid "$DAEMON_HOME"/cosmovisor/upgrades/"$SEIDVER"/bin
#################################################


# Checkout the binary for 1.0.6beta
#################################################
SEIDVER=1.0.6beta
cd $HOME || { echo "$HOME" dizinine girilemiyor"; sleep 10; exit 13;}
#remove old sei-chain directory
rm -rf sei-chain
#clone sei-chain
git clone --depth 1 https://github.com/sei-protocol/sei-chain.git
cd "sei-chain" && echo "sei-chain" dizinine girilemiyor && sleep 10 && exit 13
git fetch --tags -f
git checkout $SEIDVER
# Build the new version
make install
go build -o build/seid ./cmd/seid
# Checkout the binary for 1.0.xbeta
# if not right version exit script.
cd $HOME || { echo "$HOME" dizinine girilemiyor"; sleep 10; exit 13;}
#create cosmovisor upgrade directory
mkdir -p "$DAEMON_HOME"/cosmovisor/upgrades/"$SEIDVER"/bin
#copy new seid version correct upgrade cosmovisor directory
cp "$HOME"/go/bin/seid "$DAEMON_HOME"/cosmovisor/upgrades/"$SEIDVER"/bin
if [ ! -f "$DAEMON_HOME/cosmovisor/upgrades/$SEIDVER%20upgrade/bin/seid" ]; then
    echo "$DAEMON_HOME/cosmovisor/upgrades/$SEIDVER%20upgrade/bin/seid FILE NOT EXIST"
    read -r -s -n 1 -p "Press any key to EXIT . . ."
    exit 13
else
  SEIDBUILDVER=$("$DAEMON_HOME"/cosmovisor/upgrades/"$SEIDVER"/bin/seid version)
  if [ "$SEIDBUILDVER" == "$SEIDVER" ]; then
  	echo "$DAEMON_HOME"/cosmovisor/upgrades/"$SEIDVER"/bin/seid version "$SEIDVER"
  else
    	echo -e "\e[1m\e[31m2. Error version not match $SEIDVER \e[0m"
    	echo -e "\e[1m\e[31m2. please check $DAEMON_HOME/cosmovisor/upgrades/$SEIDVER%20upgrades/bin version file \e[0m"
    	read -r -s -n 1 -p "Press any key to EXIT . . ."
  	exit 13
  fi
fi
#################################################

cd $HOME || { echo "$HOME" dizinine girilemiyor"; sleep 10; exit 13;}
#install helpsei command
sudo wget https://raw.githubusercontent.com/snipeTR/sei_help/main/sei_help.sh && chmod +x ./sei_help.sh &&sudo mv ./sei_help.sh /usr/local/bin/helpsei

#run first cosmovisor for $HOME/.sei/cosmovisor/current/bin/seid file link create.
DAEMON_HOME=$HOME/.sei DAEMON_NAME=seid DAEMON_RESTART_AFTER_UPGRADE=true ./cosmos-sdk/cosmovisor/cosmovisor run start
sleep 3
kill "$(pidof cosmovisor)"

#remove execute file from local/bin
rm -rf /usr/local/bin/seid

#add link current seid execute to local/bin
ln -s "$HOME"/.sei/cosmovisor/current/bin/seid /usr/local/bin/seid

mkdir ~/bkup_cosmovisor_sei
echo UNSAFE_SKIP_BACKUP=true DAEMON_HOME=~/.sei DAEMON_NAME=seid DAEMON_RESTART_AFTER_UPGRADE=true DAEMON_DATA_BACKUP_DIR=~/bkup_cosmovisor_sei cosmovisor run start --home ~/.sei>seid_start_with_cosmovisor.sh
chmod +x seid_start_with_cosmovisor.sh

echo -e "\e[1m\e[32m installation complete...installation complete...installation complete \e[0m"
echo -e "\e[1m\e[32m If you want to run seid for cosmovisor, run seid_start_with_cosmovisor.sh. \e[0m"
echo -e "\e[1m\e[32m this command will do the following in order. \e[0m"
echo -e "\e[1m\e[32m First, the seid 1.0.2beta version will work up to a block height of 153759. \e[0m"
echo -e "\e[1m\e[32m When it reaches a block height of 153759, seid will run version 1.0.3beta. \e[0m"
echo -e "\e[1m\e[32m When it reaches a block height of 681000, seid will run version 1.0.4beta. \e[0m"
echo -e "\e[1m\e[32m When it reaches a block height of 1075040, seid will run version 1.0.5beta. \e[0m"
echo -e "\e[1m\e[32m When it reaches a block height of 1217302, seid will run version 1.0.6beta. \e[0m"
echo -e "\e[1m\e[32m To add the versions that will be released later, download and run the script named sei_cosmovisor_update.sh \e[0m"
