import os
import subprocess
import json
import requests
import zipfile

from io import BytesIO

# Mapping of env to chain_id
ENV_TO_CHAIN_ID = {
    "devnet": "arctic-1",
    "testnet": "atlantic-2"
}

def print_ascii_and_intro():
    print("""
                     ..:=++****++=:.                      
                  .:+*##############*+:.                  
                .=*#####+:....:+#######+.                 
              .-*#####=.  ....  .+###*:. ...              
            ..+#####=.. .=####=.  .... .-*#=.             
            .+#####+. .=########+:...:=*####=.            
            =########*#######################-            
           .#################=:...=###########.           
           ...  ..-*######+..      .:*########:           
            ..=-.   -###-    -####.   :+######:           
           :#####+:       .=########:   .+####:           
           .########+:.:=#############=-######.           
            =################################-            
            .+#####*-.. ..-########+.. ..-*#=.            
            ..+##*-. ..... .-*###-. ...... ..             
              .--. .:*###*:.  ...  .+###*-.               
                 .:+#######*-:..::*#####=.                
                  .-+###############*+:.                  
                     ..-+********+-..                     
                     
Welcome to the Sei node installer!
For more information please visit docs.sei.io
Please make sure you have the following installed locally:
\t- golang 1.19 (as well as your GOPATH set up correctly)
\t- make
\t- gcc
\t- docker
This tool will build from scratch seid and wipe away existing state. 
Please backup any important existing data before proceeding.
""")


def install_latest_release():
    response = requests.get("https://api.github.com/repos/sei-protocol/sei-chain/releases/latest")
    if response.status_code != 200:
        raise Exception(f"Error getting latest version: {response.status_code}")
    latest_version = response.json()["tag_name"]
    response = requests.get(f"https://github.com/sei-protocol/sei-chain/archive/refs/tags/{latest_version}.zip")
    if response.status_code != 200:
        raise Exception(f"Error downloading sei binary {latest_version}")
    zip_file = zipfile.ZipFile(BytesIO(response.content))
    zip_file.extractall(".")
    print("TEST" + zip_file.namelist()[0])
    os.chdir(zip_file.namelist()[0])
    run_command("make install")


# Grab RPC from chain-registry
def get_rpc_server(chain_id):
    chains_json_url = "https://raw.githubusercontent.com/sei-protocol/chain-registry/main/chains.json"
    response = requests.get(chains_json_url)
    chains = response.json()
    rpcs = []
    for chain in chains:
        if chains[chain]['chainId'] == chain_id:
            rpcs = chains[chain]['rpc']
            break
    # check connectivity
    for rpc in rpcs:
        try:
            response = requests.get(rpc['url'])
            if response.status_code == 200:
                return rpc['url']
        except Exception:
            pass

    return None


def take_manual_inputs():
    # Manual input prompts
    print(
        """Please choose an environment:
        1. devnet (arctic-1)
        2. testnet (atlantic-2)"""
    )
    choice = input("Enter choice:")
    while choice not in ['1', '2']:
        print("Invalid input. Please enter '1' or '2'.")
        choice = input("Enter choice:")

    env = "devnet" if choice == "1" else "testnet"
    moniker = input("Enter moniker: ")
    return env, moniker

def get_state_sync_params(rpc_url):
    trust_height_delta = 40000 # may need to tune
    response = requests.get(f"{rpc_url}/status")
    latest_height = int(response.json()['sync_info']['latest_block_height'])
    sync_block_height = latest_height - trust_height_delta if latest_height > trust_height_delta else latest_height
    response = requests.get(f"{rpc_url}/block?height={sync_block_height}")
    sync_block_hash = response.json()['block_id']['hash']
    return sync_block_height, sync_block_hash

def get_persistent_peers(rpc_url):
    with open(os.path.expanduser('~/.sei/config/node_key.json'), 'r') as f:
        self_id = json.load(f)['id']
        response = requests.get(f"{rpc_url}/net_info")
        peers = [peer['url'].replace('mconn://', '') for peer in response.json()['peers'] if peer['node_id'] != self_id]
        persistent_peers = ','.join(peers)
        return persistent_peers

def get_genesis_file(chain_id):
    genesis_url = f"https://raw.githubusercontent.com/sei-protocol/testnet/main/{chain_id}/genesis.json"
    response = requests.get(genesis_url)
    return response



def run_command(command):
    subprocess.run(command, shell=True, check=True)


def main():
    print_ascii_and_intro()
    # env, node_type, moniker = take_manual_inputs()
    # TODO remove
    env, moniker = "devnet", "hpilip"
    print(f"Setting up a node in {env} with moniker: {moniker}")
    chain_id = ENV_TO_CHAIN_ID[env]
    rpc_url = get_rpc_server(chain_id)
    rpc_url="http://18.191.12.108:26657"

    # Install binary
    install_latest_release()

    # Remove previous sei data
    run_command("rm -rf $HOME/.sei")
    # Init seid
    run_command(f"seid init --chain-id {chain_id} {moniker}")
    sync_block_height, sync_block_hash = get_state_sync_params(rpc_url)
    persistent_peers = get_persistent_peers(rpc_url)
    genesis_file = get_genesis_file(chain_id)

    # Set genesis and configs
    config_path = os.path.expanduser('~/.sei/config/config.toml')

    genesis_path = os.path.join(os.path.dirname(config_path), 'genesis.json')
    with open(genesis_path, 'wb') as f:
        f.write(genesis_file.content)

    config_path = os.path.expanduser('~/.sei/config/config.toml')
    with open(config_path, 'r') as file:
        config_data = file.read()

    config_data = config_data.replace('rpc-servers = ""', f'rpc-servers = "{rpc_url},{rpc_url}"')
    config_data = config_data.replace('trust-height = 0', f'trust-height = {sync_block_height}')
    config_data = config_data.replace('trust-hash = ""', f'trust-hash = "{sync_block_hash}"')
    config_data = config_data.replace('persistent-peers = ""', f'persistent-peers = "{persistent_peers}"')
    config_data = config_data.replace('enable = false', 'enable = true')
    config_data = config_data.replace('db-sync-enable = true', 'db-sync-enable = false')
    config_data = config_data.replace('use-p2p = false', 'use-p2p = true')

    with open(config_path, 'w') as file:
        file.write(config_data)

    # Start seid
    print("Starting seid...")
    run_command("seid start")


main()