#!/bin/bash

#  discord
#
#  snipeTR#8374 & karboran#2719
# beta1.5v

echo tnx for kj89
sleep 1
cd "$HOME" || { echo "Unable to enter $HOME directory"; sleep 1; exit 13;}
#update script download
if [ -f sei_cosmovisor_update.sh ]; then rm -rf sei_cosmovisor_update.sh; fi
wget -O "sei_cosmovisor_update.sh" "https://raw.githubusercontent.com/snipeTR/cosmos_sei_install/main/sei_cosmovisor_update.sh> /dev/null 2>&1 && chmod +x sei_cosmovisor_update.sh"

if [ -f ".bash_profile" ]; then 
		if [ -f ".bsh_profil_org" ]; then
			 cp .bash_profile .bsh_profile_org
		fi
fi
if [ -f ".bash_profile" ]; then source "$HOME"/.bash_profile; fi

# delete old line for dublicate.

if [ -f ".bash_profile" ]; then 
sed -i.org '/DAEMON_DATA_BACKUP_DIR\|DAEMON_RESTART_AFTER_UPGRADE\|DAEMON_NAME\|DAEMON_HOME\|UNSAFE_SKIP_BACKUP\|SEI_PORT\|SEI_CHAIN_ID\|WALLET\|NODENAME/d' "$HOME/.bash_profile"
fi

# set vars
if [ "$NODENAME" ]; then 
  echo -e "\nNode name \e[1m\e[32mseting before\e[0m, NODE NAME..:\e[1m\e[32m$NODENAME\e[0m"
  echo -e "Press [ANY KEY] to use the \e[1m\e[32msame node name\e[0m."
  echo -e "Press [Y/y] to change \e[1m\e[32m$NODENAME\e[0m."
   read -rsn1 answer
    if [ "$answer" != "${answer#[Yy]}" ] ;then
    	NODENAME=''
    	 while [ ! "$(echo $NODENAME | wc -m)" -gt "1" ]; do
         	read -p "Enter Node name: " NODENAME
         	if [ ! "$(echo $NODENAME | wc -m)" -gt "1" ]; then echo -e "\e[1m\e[31m*** Node name cannot be empty.\e[0m" ; fi
         done 
      echo "export NODENAME=$NODENAME" >> "$HOME"/.bash_profile
    fi
  else
    while [ ! "$(echo $NODENAME | wc -m)" -gt "1" ]; do
       	read -p "Enter Node name: " NODENAME
       	if [ ! "$(echo $NODENAME | wc -m)" -gt "1" ]; then echo -e "\e[1m\e[31m*** Node name cannot be empty.\e[0m" ; fi
       done 
fi
echo "export NODENAME=$NODENAME" >> "$HOME"/.bash_profile

if [ ! "$SEI_PORT" ]; then SEI_PORT=12; fi

if [ "$WALLET" ]; then 
	echo -e "\nWallet name \e[1m\e[32mseting before\e[0m, WALLET NAME..:\e[1m\e[32m$WALLET\e[0m"
  echo -e "Press ANY KEY to use the \e[1m\e[32msame wallet name\e[0m."
  echo -e "Press [Y/y] to change \e[1m\e[32m$WALLET\e[0m."
   read -rsn1 answer
    if [ "$answer" != "${answer#[Yy]}" ] ;then
    	WALLET=''
    	 while [ ! "$(echo $WALLET | wc -m)" -gt "1" ]; do 
         	read -p "Enter Wallet name: " WALLET
         	if [ ! "$(echo $WALLET | wc -m)" -gt "1" ] ; then echo -e "\e[1m\e[31m*** Wallet name cannot be empty.\e[0m" ; fi
         done 
      echo "export WALLET=$WALLET" >> "$HOME"/.bash_profile
    fi
  else
       while [ ! "$(echo $WALLET | wc -m)" -gt "1" ]; do
       	read -p "Enter Wallet name: " WALLET
       	if [ ! "$(echo $WALLET | wc -m)" -gt "1" ]; then echo -e "\e[1m\e[31m*** Wallet name cannot be empty.\e[0m" ; fi
       done 
fi
echo "export WALLET=$WALLET" >> "$HOME"/.bash_profile

echo "export SEI_CHAIN_ID=atlantic-1" >> "$HOME"/.bash_profile
echo "export SEI_PORT=${SEI_PORT}" >> "$HOME"/.bash_profile

echo "export UNSAFE_SKIP_BACKUP=true" >> "$HOME"/.bash_profile
echo "export DAEMON_HOME=~/.sei" >> "$HOME"/.bash_profile
echo "export DAEMON_NAME=seid" >> "$HOME"/.bash_profile
echo "export DAEMON_RESTART_AFTER_UPGRADE=true" >> "$HOME"/.bash_profile
echo "export DAEMON_DATA_BACKUP_DIR=~/bkup_cosmovisor_sei" >> "$HOME"/.bash_profile

source "$HOME"/.bash_profile

echo '================================================='
echo -e "Your node name(moniker): \e[1m\e[32m$NODENAME\e[0m"
echo -e "Your wallet name: \e[1m\e[32m$WALLET\e[0m"
echo -e "Your chain name: \e[1m\e[32m$SEI_CHAIN_ID\e[0m"
echo -e "Your port: \e[1m\e[32m$SEI_PORT\e[0m"
echo '================================================='
   echo -e "\e[1m\e[35mPlease check the accuracy of the information \e[1m\e[36mCAREFULLY.\e[0m"
   echo -e "\e[1m\e[31mAre the above values correct? [Y/N]\e[0m"
   read -rsn1 answer
    if [ "$answer" != "${answer#[Yy]}" ] ;then
      echo Yes
    else
      echo No
      sleep 3
    exit 13
    fi
echo -e "\e[1m\e[32m1. Updating packages... \e[0m" && sleep 1
# update
sudo apt update && sudo apt upgrade -y

echo -e "\e[1m\e[32m2. Installing dependencies... \e[0m" && sleep 1
# packages
sudo apt-get install software-properties-common -y
sudo add-apt-repository ppa:bashtop-monitor/bashtop -y
sudo apt install curl build-essential git wget jq make gcc tmux tree mc software-properties-common net-tools bashtop qrencode htop -y

# install go function
installgo () 
  {
	  cd "$HOME" || { echo "Unable to enter $HOME directory";}
	  ver="$1"
	  wget "https://golang.org/dl/go$ver.linux-amd64.tar.gz"
	  sudo rm -rf /usr/local/go
	  sudo tar -C /usr/local -xzf "go$ver.linux-amd64.tar.gz"
	  rm "go$ver.linux-amd64.tar.gz"
	  sed -i.org '/GOROOT\|GOPATH\|GO111MODULE/d' "$HOME/.bash_profile"
	  echo "export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin" >> ~/.bash_profile
	  echo "export GOROOT=/usr/local/go" >> "$HOME"/.bash_profile
	  echo "export GOPATH=$HOME/go" >> "$HOME"/.bash_profile
	  echo "export GO111MODULE=on" >> "$HOME"/.bash_profile
	  source "$HOME"/.bash_profile
	  echo "++++++++" && echo -e "\e[1m\e[32m Go installation complate... \e[0m" && echo -e "\e[1m\e[32m $(go version) \e[0m" && echo "++++++++" && sleep 1
	}

#go version variable, if need new version change variable
ver="1.18.2"

# /usr/local/go/bin/go
if [ -f "/usr/local/go/bin/go" ]; then
    checkgover=$(go version)
# /usr/local/go
elif [ -f "/usr/local/go" ]; then
	if [ ! -n "$checkgover" ]; then
    checkgover=$(go version)
  fi
else
  checkgover="NOT.HERE.GO.IM.SORY"
fi

if [ "${checkgover:13:4}" == "${ver:0:4}" ]; then 
	#NO NEED install go
	echo -e "\e[1m\e[32mNo need to install go-lang\e[0m, versions are compatible."
	echo -e "\e[1m\e[32mrequested\e[0m version $ver, \e[1m\e[32minstalled\e[0m version ${checkgover:13:6}"
else
  #installation required
  installgo "$ver"
fi

# download binary
echo -e "\e[1m\e[32m3. Downloading and building binaries... \e[0m" && sleep 1
cd "$HOME" || { echo "Unable to enter $HOME directory"; sleep 1; exit 13;}
#remove old sei-chain directory
DIR="sei-chain"
if [ -d "$DIR" ]; then
  # Take action if $DIR exists. #
  echo -e "\e[1m\e[32m2. remove old sei-chain directory... \e[0m" && sleep 1
  sudo rm -rf sei-chain
fi
git clone https://github.com/sei-protocol/sei-chain.git
cd sei-chain || { echo "Unable to enter sei-chain directory"; sleep 1; exit 13;}
git checkout 1.0.6beta
make install 
sudo cp ~/go/bin/seid /usr/local/bin/seid
echo -e "\e[1m\e[32m  seid build end... \e[0m"

# config
echo -e "\e[1m\e[32m-- seid config chain-id "$SEI_CHAIN_ID" \e[0m"
seid config chain-id "$SEI_CHAIN_ID"
echo -e "\e[1m\e[32m-- seid config keyring-backend test \e[0m"
seid config keyring-backend test
echo -e "\e[1m\e[32m-- seid config node tcp://localhost:"${SEI_PORT}657" \e[0m"
seid config node tcp://localhost:"${SEI_PORT}657"

# init
echo -e "\e[1m\e[32m-- seid init "$NODENAME" --chain-id "$SEI_CHAIN_ID" \e[0m"
seid init "$NODENAME" --chain-id "$SEI_CHAIN_ID"

# reset
seid tendermint unsafe-reset-all --home "$HOME"/.sei

# download genesis and addrbook
wget -qO $HOME/.sei/config/genesis.json "https://raw.githubusercontent.com/sei-protocol/testnet/main/sei-incentivized-testnet/genesis.json"
wget -qO $HOME/.sei/config/addrbook.json "https://raw.githubusercontent.com/sei-protocol/testnet/main/sei-incentivized-testnet/addrbook.json"

# set custom ports
sed -i.bak -e "s%^proxy_app = \"tcp://127.0.0.1:26658\"%proxy_app = \"tcp://127.0.0.1:${SEI_PORT}658\"%; s%^laddr = \"tcp://127.0.0.1:26657\"%laddr = \"tcp://127.0.0.1:${SEI_PORT}657\"%; s%^pprof_laddr = \"localhost:6060\"%pprof_laddr = \"localhost:${SEI_PORT}060\"%; s%^laddr = \"tcp://0.0.0.0:26656\"%laddr = \"tcp://0.0.0.0:${SEI_PORT}656\"%; s%^prometheus_listen_addr = \":26660\"%prometheus_listen_addr = \":${SEI_PORT}660\"%" "$HOME"/.sei/config/config.toml
sed -i.bak -e "s%^address = \"tcp://0.0.0.0:1317\"%address = \"tcp://0.0.0.0:${SEI_PORT}317\"%; s%^address = \":8080\"%address = \":${SEI_PORT}080\"%; s%^address = \"0.0.0.0:9090\"%address = \"0.0.0.0:${SEI_PORT}090\"%; s%^address = \"0.0.0.0:9091\"%address = \"0.0.0.0:${SEI_PORT}091\"%" "$HOME"/.sei/config/app.toml

#port_description file
sudo rm -rf usr/local/bin/seiport
echo -e "\e[1m\e[32m create seiport command /usr/local/bin \e[0m" && sleep 3
echo echo curl -s localhost:${SEI_PORT}657/status >seiport
echo echo proxy_app = :${SEI_PORT}658 >>seiport
echo echo laddr = :${SEI_PORT}657 >>seiport
echo echo pprof_laddr = :${SEI_PORT}060 >>seiport
echo echo laddr = :${SEI_PORT}656 >>seiport
echo echo prometheus_listen_addr = :${SEI_PORT}660 >>seiport
echo echo address = :${SEI_PORT}317 >>seiport
echo echo address = :${SEI_PORT}080 >>seiport
echo echo address = :${SEI_PORT}090 >>seiport
echo echo address = :${SEI_PORT}091 >>seiport
chmod +x ./seiport
sudo mv ./seiport /usr/local/bin

# disable indexing
indexer="null"
sed -i -e "s/^indexer *=.*/indexer = \"$indexer\"/" "$HOME"/.sei/config/config.toml

# config pruning
pruning="custom"
pruning_keep_recent="100"
pruning_keep_every="0"
pruning_interval="50"
sed -i -e "s/^pruning *=.*/pruning = \"$pruning\"/" "$HOME"/.sei/config/app.toml
sed -i -e "s/^pruning-keep-recent *=.*/pruning-keep-recent = \"$pruning_keep_recent\"/" "$HOME"/.sei/config/app.toml
sed -i -e "s/^pruning-keep-every *=.*/pruning-keep-every = \"$pruning_keep_every\"/" "$HOME"/.sei/config/app.toml
sed -i -e "s/^pruning-interval *=.*/pruning-interval = \"$pruning_interval\"/" "$HOME"/.sei/config/app.toml

# set minimum gas price (developers don't want 0usei min gasfee.)
sed -i -e "s/^minimum-gas-prices *=.*/minimum-gas-prices = \"10usei\"/" "$HOME"/.sei/config/app.toml

# enable prometheus
sed -i -e "s/prometheus = false/prometheus = true/" "$HOME"/.sei/config/config.toml

echo -e "\e[1m\e[32m4. Creating service... \e[0m" && sleep 1
# create service
sudo tee /etc/systemd/system/seid.service > /dev/null <<EOF
[Unit]
Description=sei
After=network-online.target

[Service]
User=$USER
ExecStart=$(which seid) start --home "$HOME"/.sei
Restart=on-failure
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

# start service
echo -e "\e[1m\e[32mservice command info... \e[0m"
sudo systemctl daemon-reload
echo -e "echo sudo systemctl enable seid\t\tfor automatic service start when the server is up"
echo -e "echo sudo systemctl disable seid\t\tdisable for automatic service start when the server is up"
echo -e "echo sudo systemctl start seid\t\tstarting validator for linux service \e[1m\e[31mplease dont use run cosmovisor\e[0m"
echo -e "echo sudo systemctl stop seid\t\tstop validator"
echo -e "echo sudo systemctl restart seid\t\tre start validator"
echo -e "\e[1m\e[32mNOTE:Please do not use these services while working with cosmovisor.\e[0m"

echo -e "\e[1m\e[32m5. installing Cosmovisor... \e[0m" && sleep 1

cd "$HOME" || { echo "Unable to enter $HOME directory"; sleep 1; exit 13;}
rm -rf ./cosmos-sdk
git clone --depth 1 --branch main https://github.com/cosmos/cosmos-sdk
cd cosmos-sdk || { echo "Unable to enter cosmos-sdk directory"; sleep 1; exit 13;}
make cosmovisor

#check binary cosmovisor
if [ ! -f "$HOME/cosmos-sdk/cosmovisor/cosmovisor" ]; then
    echo "cosmovisor not build. \e[1m\e[31mERROR ERROR\e[0m"
    read -r -s -n 1 -p "Press any key to EXIT . . ."
  	exit 13
fi
mkdir -p "$HOME"/.sei/cosmovisor/genesis/bin
cp "$HOME"/go/bin/seid "$HOME"/.sei/cosmovisor/genesis/bin
if [ -f "$HOME/.sei/cosmovisor/genesis/bin/seid" ]; then
     echo "$HOME"/.sei/cosmovisor/genesis/bin/seid file copy successful
     sleep 2
else
      	echo -e "\e[1m\e[31m ERROR $HOME/go/bin/seid not copy $HOME/.sei/cosmovisor/genesis/bin \e[0m"
      	echo -e "\e[1m\e[31m please check $HOME/go/bin/seid file \e[0m"
      	read -r -s -n 1 -p "Press any key to EXIT . . ."
    	exit 13
fi
cd "$HOME" || { echo "Unable to enter $HOME directory"; sleep 1; exit 13;}

#install helpsei and helpseiupdate command
sudo wget https://raw.githubusercontent.com/snipeTR/sei_help/main/sei_help.sh && chmod +x ./sei_help.sh &&sudo mv ./sei_help.sh /usr/local/bin/helpsei
sudo wget https://raw.githubusercontent.com/snipeTR/sei_help/main/helpseiupdate && chmod +x ./helpseiupdate &&sudo mv ./helpseiupdate /usr/local/bin/helpseiupdate

#Crontab remove old helpseiupdate
crontab -l | grep -v 'sudo /usr/local/bin/helpseiupdate'  | crontab -

#Crontab remove add helpseiupdate evry 00:00, 06:00, 12:00, 18:00 hour update helpsei
(crontab -l ; echo "0 0,6,12,18 * * * sudo /usr/local/bin/helpseiupdate") | crontab -


#run first cosmovisor for $HOME/.sei/cosmovisor/current/bin/seid file link create.
sudo cp "./cosmos-sdk/cosmovisor/cosmovisor" "/usr/local/bin/"
echo "please wait..."
DAEMON_HOME=$HOME/.sei DAEMON_NAME=seid DAEMON_RESTART_AFTER_UPGRADE=true ./cosmos-sdk/cosmovisor/cosmovisor run start& > /dev/null 2>&1
sleep 4
kill "$(pidof cosmovisor)"
wait

#remove execute file from local/bin
sudo rm -rf /usr/local/bin/seid

#add link current seid execute to local/bin
sudo ln -s "$HOME"/.sei/cosmovisor/current/bin/seid /usr/local/bin/seid

mkdir ~/bkup_cosmovisor_sei
echo "ulimit -n 1000000" >seid_start_with_cosmovisor.sh
echo ""if" [ ! \"\$(systemctl is-active seid)\" == \"inactive\" ]; "then" systemctl stop seid && systemctl disable seid && echo -e \"\\e[1m\\e[36mseid service has been shut down and disabled.\\n \\e[0m \"; "fi"" >>seid_start_with_cosmovisor.sh
echo ""if" [ \"\$(systemctl is-active seid)\" == \"inactive\"  ]; "then" echo -e \"\\e[1m\\e[36mseid service is not running, no need to turn off seid service.\\n \\e[0m \"; "fi"" >>seid_start_with_cosmovisor.sh
echo "pgrep cosmovisor >/dev/null 2>&1" >>seid_start_with_cosmovisor.sh
echo ""if" [ \"\$?\" -eq \"0\" ]; "then" echo -e \"\\e[1m\\e[31m\\n\\n\\n-Currently cosmovisor is already running..\\n-if you don't know what you are doing\\nplease close the running cosmovisor and try again.\\n-press \\e[1m\\e[32m[F/f]\\e[1m\\e[31m to go ahead and \\e[1m\\e[36mforce cosmovisor to run.\\n\\e[1m\\e[31m-press \\e[1m\\e[32mANY KEY\\e[1m\\e[31m run it again cosmovisor \\e[1m\\e[36m(The currently running cosmovisor is terminated.) \\n\\n\\n\\e[0m\"; "fi"" >>seid_start_with_cosmovisor.sh
echo "read -rsn1 answer" >>seid_start_with_cosmovisor.sh
echo  ""if" [ \""\$"answer\" == \"\${answer#[Ff]}\" ] ; "then" pkill cosmovisor; "fi"" >>seid_start_with_cosmovisor.sh
echo "UNSAFE_SKIP_BACKUP=true DAEMON_HOME=~/.sei DAEMON_NAME=seid DAEMON_RESTART_AFTER_UPGRADE=true DAEMON_DATA_BACKUP_DIR=~/bkup_cosmovisor_sei cosmovisor run start init ~/.sei" >>seid_start_with_cosmovisor.sh
chmod +x seid_start_with_cosmovisor.sh

echo "ulimit -n 1000000" >seid_start_with_service.sh
echo "pgrep cosmovisor >/dev/null 2>&1" >>seid_start_with_service.sh
echo ""if" [ \"\$?\" -eq \"0\" ]; "then" echo -e \"\\e[1m\\e[31m\\n\\n\\n-Currently cosmovisor is already running..\\n-cosmovisor shutdown..\\n\\e[0m\" && pkill cosmovisor; "fi"" >>seid_start_with_service.sh
echo "sleep 2" >>seid_start_with_service.sh
echo "pgrep cosmovisor >/dev/null 2>&1" >>seid_start_with_service.sh
echo ""if" [ \"\$?\" -eq \"0\" ]; "then" echo -e \"\\e[1m\\e[31m\\n-Failed closing cosmovisor...\\n-cannot be terminated..\\n\\e[0m\" && exit 2; "fi"" >>seid_start_with_service.sh
echo "systemctl enable seid" >>seid_start_with_service.sh
echo "systemctl start seid" >>seid_start_with_service.sh
echo "sleep 3" >>seid_start_with_service.sh
echo ""if" [ \"\$(systemctl is-active seid)\" == \"inactive\"  ]; "then" echo -e \"\\e[1m\\e[31mseid service is not running, \\nTry running the script again.\\n\\e[0m \" && exit 2; "fi"" >>seid_start_with_service.sh
echo ""if" [ ! \"\$(systemctl is-active seid)\" == \"inactive\" ]; "then" echo -e \"\\e[1m\\e[32mseid service is currently running.\\e[0m\"; "fi"" >>seid_start_with_service.sh
chmod +x seid_start_with_service.sh


echo '=============== SETUP FINISHED ==================='
echo -e "\n\e[1m\e[32mHere are some \e[0mCOMANDS\e[1m\e[32m that will make your validator job easier.\n\n\e[1m\e[32mhelpsei\n\e[1m\e[34mIt gives information about some commands about sei-chain validator.\nseid service or application must be running.\n\n\e[1m\e[32mhelpseiupdate\n\e[1m\e[34mhelpsei updates the command.\n\n\e[1m\e[32mseiport\n\e[1m\e[34mLists all ports used by seid.\ndetailed information in $HOME/.sei/config/config.toml\e[0m\n\n\n"
sleep 2
echo -e "Do you want to create wallets? [Y/N]"
   read -rsn1 answer
    if [ "$answer" != "${answer#[Yy]}" ] ;then
    	   echo -e "press \e[1m\e[32m[Y]\e[0m for \e[1m\e[34mnew wallet\e[0m. \nPress \e[1m\e[32m[any key]\e[0m for \e[1m\e[34mrecover wallet\e[0m with mnemonic.\e[0m"
         read -rsn1 aanswer
         if [ "$aanswer" != "${aanswer#[Yy]}" ] ;then
         	  if [[ $(seid keys list --output json | jq .[0].name) == "\"$WALLET"\" || $(seid keys list --output json | jq .[1].name) == "\"$WALLET"\" || $(seid keys list --output json | jq .[2].name) == "\"$WALLET"\" || $(seid keys list --output json | jq .[3].name) == "\"$WALLET"\" ]]; then 
         		  echo -e "The wallet named..:\e[1m\e[34m$WALLET\e[0m is already installed on your system,"
         		  read -p "Enter \e[1m\e[34mnew\e[0m wallet name: " WALLETT
         		  
         		  echo "export WALLET=$WALLET" >> "$HOME"/.bash_profile
         		  seid keys add $WALLETT
         	  else
         	    seid keys add $WALLET
         	  fi
         	 echo -e "\n\e[0m\e[31mThe top line is 24 words.\e[0m \e[0m\e[36mThese words are a secret, do not publish, do not show in public.\e[0m\n\n\n"
         else
           echo -e "\n\e[1m\e[32mPlease enter the recovery words for wallet.\e[0m wallet name..: \e[1m\e[35m$WALLET.\e[0m"
           RET=987
           until [ ${RET} -eq 0 ]; do
              if [ ! ${RET} -eq 987 ]; then echo "\e[1m\e[31mYour recovery words are incorrect, please re-enter carefully.\e[0m"; fi
              seid keys add $WALLET --recover
              RET=$?
           done
         fi
    else
      echo Your answer No. please create wallet
      sleep 1
    exit 0
    fi

echo "----------------------------------------------------"
echo -e "\e[0m\e[36mThis is a testnet. you need sei token to create validator. \nFor detailed information, I recommend you to join the sei-chain official discord group.\n \e[1m\e[32mhttps://discord.gg/vcCTGnqTW6\e[0m"
echo -e "\e[0m\e[36mYou can get detailed information about SEI NODE commands with the helpsei command.\e[0m"
echo -e "\e[0m\e[01mIf you want to run sei-chain atlantic-1 NODE with cosmovisor. Run the script \e[0m\e[36m"seid_start_with_cosmovisor.sh".\e[0m"
echo -e "\e[0m\e[01mIf you want to run sei-chai atlantic-1 NODE with linux services. Run the script \e[0m\e[36m"seid_start_with_service.sh".\e[0m"
echo "----------------------------------------------------"
