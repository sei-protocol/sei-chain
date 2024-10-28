#!/usr/bin/env python3

import logging
import os
import signal
import subprocess
import sys
import requests
import json
import zipfile
from io import BytesIO

# Set up logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')

# Advanced user configs
moniker = "pynode"  # Custom moniker for the node
trust_height_delta = 20000  # Negative height offset for state sync
enable_unsafe_reset = True  # Wipe database and keys before setup
version_override = False  # Override version fetching. if true, specify version(s) below

# Chain binary version ["version_override" must be true to use]
MAINNET_VERSION = "v3.9.0"
DEVNET_VERSION = "v5.2.2"
TESTNET_VERSION = "v5.2.2"

# Map env to chain ID and optional manual version override
ENV_TO_CONFIG = {
    "local": {"chain_id": None, "version": "latest"},
    "devnet": {"chain_id": "arctic-1", "version": DEVNET_VERSION},
    "testnet": {"chain_id": "atlantic-2", "version": TESTNET_VERSION},
    "mainnet": {"chain_id": "pacific-1", "version": MAINNET_VERSION}
}

def print_ascii_and_intro():
    logging.info("""
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
                     ..-+********+-.

Welcome to the Sei node installer!
For more information please visit docs.sei.io
Please make sure you have the following installed locally:
\t- golang 1.21 (with PATH and GOPATH set properly)
\t- make
\t- gcc
\t- docker
This tool will build from scratch seid and wipe away existing state.
Please backup any important existing data before proceeding.
""")

def signal_handler(sig, frame):
    logging.info("Process interrupted by user. Exiting gracefully...")
    sys.exit(0)

signal.signal(signal.SIGINT, signal_handler)
signal.signal(signal.SIGTERM, signal_handler)


# User setup prompts
def take_manual_inputs():
    env = input("Choose an environment (1: local, 2: devnet, 3: testnet, 4: mainnet): ")
    while env not in ['1', '2', '3', '4']:
        logging.warning("Invalid input. Please enter '1', '2', '3', or '4'.")
        env = input("Choose an environment: ")

    env = ["local", "devnet", "testnet", "mainnet"][int(env) - 1]
    db_choice = input("Choose the database backend (1: legacy [default], 2: sei-db): ").strip() or "1"
    if db_choice not in ["1", "2"]:
        db_choice = "1"  # Default to "1" if the input is invalid or empty
    return env, db_choice

# Fetch chain data
def get_rpc_server(chain_id):
    chains_json_url = "https://raw.githubusercontent.com/sei-protocol/chain-registry/main/chains.json"
    response = requests.get(chains_json_url)
    if response.status_code != 200:
        logging.error("Failed to retrieve chain information.")
        return None

    try:
        chains = response.json()
    except json.JSONDecodeError:
        logging.error("JSON decoding failed")
        return None

    # Fetch chain info by chain_id
    chain_info = chains.get(chain_id)
    if not chain_info:
        logging.error("Chain ID not found in the registry.")
        return None

    # Fetch and use first rpc that responds
    rpcs = chain_info.get('rpc', [])
    for rpc in rpcs:
        rpc_url = rpc.get('url')
        try:
            if requests.get(rpc_url).status_code == 200:
                return rpc_url
        except requests.RequestException as e:
            logging.error(f"Failed to connect to RPC server {rpc_url}: {e}")
            continue  # Try next url if current one fails

    return None

# Fetch latest version from GitHub for local environment
def fetch_latest_version():
    try:
        response = requests.get("https://api.github.com/repos/sei-protocol/sei-chain/releases/latest")
        response.raise_for_status()
        latest_version = response.json()["tag_name"]
        logging.info(f"Fetched latest version {latest_version} from GitHub API")
        return latest_version
    except Exception as e:
        logging.error(f"Failed to fetch latest version from GitHub API: {e}")
        sys.exit(1)

# Install release based on version tag.
def install_release(version):
    try:
        zip_url = f"https://github.com/sei-protocol/sei-chain/archive/refs/tags/{version}.zip"
        response = requests.get(zip_url)
        response.raise_for_status()
        zip_file = zipfile.ZipFile(BytesIO(response.content))
        zip_file.extractall(".")

        os.chdir(zip_file.namelist()[0])
        subprocess.run(["make", "install"], check=True)
        logging.info(f"Successfully installed version: {version}")

    except requests.exceptions.HTTPError as e:
        logging.error(f"HTTP error occurred: {e}")  # Handle http error
        sys.exit(1)
    except requests.exceptions.RequestException as e:
        logging.error(f"Error downloading files: {e}")  # Handle other errors
        sys.exit(1)
    except zipfile.BadZipFile:
        logging.error("Error unzipping file. The downloaded file may be corrupt.")
        sys.exit(1)
    except subprocess.CalledProcessError as e:
        logging.error(f"Installation failed during 'make install': {e}")
        sys.exit(1)
    except Exception as e:
        logging.error(f"An unexpected error occurred: {e}")
        sys.exit(1)

# Fetch version from RPC unless "version_override = true"
def fetch_node_version(rpc_url):
    if not version_override:
        try:
            response = requests.get(f"{rpc_url}/abci_info")
            response.raise_for_status()
            version = response.json()['response']['version']
            logging.info(f"Fetched node version {version} from {rpc_url}")
            return version
        except Exception as e:
            logging.error(f"Failed to fetch node version from RPC URL {rpc_url}: {e}")
            return None
    else:
        logging.info("Using user-specified version override.")
        return None

# Fetch state sync params
def get_state_sync_params(rpc_url):
    response = requests.get(f"{rpc_url}/status")
    latest_height = int(response.json()['sync_info']['latest_block_height'])
    sync_block_height = latest_height - trust_height_delta if latest_height > trust_height_delta else latest_height
    response = requests.get(f"{rpc_url}/block?height={sync_block_height}")
    sync_block_hash = response.json()['block_id']['hash']
    return sync_block_height, sync_block_hash

# Fetch peers list
def get_persistent_peers(rpc_url):
    node_key_path = os.path.expanduser('~/.sei/config/node_key.json')
    with open(node_key_path, 'r') as f:
        self_id = json.load(f)['id']
        response = requests.get(f"{rpc_url}/net_info")
        peers = [peer['url'].replace('mconn://', '') for peer in response.json()['peers'] if peer['node_id'] != self_id]
        persistent_peers = ','.join(peers)
        return persistent_peers

# Fetch and write genesis file directly from source
def write_genesis_file(chain_id):
    genesis_url = f"https://raw.githubusercontent.com/sei-protocol/testnet/main/{chain_id}/genesis.json"
    response = requests.get(genesis_url)
    if response.status_code == 200:
        genesis_path = os.path.expanduser('~/.sei/config/genesis.json')
        with open(genesis_path, 'wb') as file:
            file.write(response.content)
        logging.info("Genesis file written successfully.")
    else:
        logging.error(f"Failed to download genesis file: HTTP {response.status_code}")

def run_command(command):
    try:
        subprocess.run(command, shell=True, check=True)
    except subprocess.CalledProcessError as e:
        logging.error(f"Command '{command}' failed with return code {e.returncode}")
    except KeyboardInterrupt:
        logging.info("Process interrupted by user. Exiting gracefully...")

def ensure_file_path(file_path):
    os.makedirs(os.path.dirname(file_path), exist_ok=True)
    if not os.path.exists(file_path):
        open(file_path, 'a').close()
        logging.info(f"Created missing file: {file_path}")

def main():
    try:
        # Register signal handlers
        signal.signal(signal.SIGINT, signal_handler)
        signal.signal(signal.SIGTERM, signal_handler)

        print_ascii_and_intro()
        env, db_choice = take_manual_inputs()
        logging.info(f"Setting up a node in {env}")

        # Fetch chain_id and version from ENV_TO_CONFIG
        config = ENV_TO_CONFIG[env]
        chain_id = config['chain_id']
        version = config['version']

        if env == "local":
            # Fetch latest version from GitHub for local setup
            version = fetch_latest_version()
            chain_id = "local"  # Set chain_id for local setup
        else:
            # Determine version by RPC or override
            rpc_url = get_rpc_server(chain_id)
            dynamic_version = fetch_node_version(rpc_url) if not version_override else None
            version = dynamic_version or version  # Use the fetched version if not overridden

        # Install selected release
        install_release(version)

        # Unsafe-reset-all only if directory exists, and by config at top of script
        home_dir = os.path.expanduser('~/.sei')
        if enable_unsafe_reset and os.path.exists(home_dir):
            try:
                subprocess.run(["seid", "tendermint", "unsafe-reset-all"], check=True)
            except subprocess.CalledProcessError as e:
                logging.error(f"Failed to execute 'seid tendermint unsafe-reset-all': {e}")
                sys.exit(1)

        # Clean up previous data, init seid with given chain ID and moniker
        subprocess.run(["rm", "-rf", home_dir])
        subprocess.run(["seid", "init", moniker, "--chain-id", chain_id], check=True)

        if env == "local":
            logging.info("Running local initialization script...")
            local_script_path = os.path.expanduser('~/sei-chain/scripts/initialize_local_chain.sh')
            run_command(f"chmod +x {local_script_path}")
            run_command(local_script_path)
        else:
            # Fetch state-sync params and persistent peers
            sync_block_height, sync_block_hash = get_state_sync_params(rpc_url)
            persistent_peers = get_persistent_peers(rpc_url)

            # Fetch and write genesis
            write_genesis_file(chain_id)

            # Config changes
            config_path = os.path.expanduser('~/.sei/config/config.toml')
            app_config_path = os.path.expanduser('~/.sei/config/app.toml')

            # Confirm exists before modifying config files
            ensure_file_path(config_path)
            ensure_file_path(app_config_path)

            # Read and modify config.toml
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

            # Read modify and write app.toml if sei-db is selected
            if db_choice == "2":
                with open(app_config_path, 'r') as file:
                    app_data = file.read()
                app_data = app_data.replace('sc-enable = false', 'sc-enable = true')
                app_data = app_data.replace('ss-enable = false', 'ss-enable = true')
                with open(app_config_path, 'w') as file:
                    file.write(app_data)

        # Start seid
        logging.info("Starting seid...")
        run_command("seid start")
    except KeyboardInterrupt:
        logging.info("Main process interrupted by user. Exiting gracefully...")

if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        logging.info("Script interrupted by user. Exiting gracefully...")
