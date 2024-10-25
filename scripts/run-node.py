#!/usr/bin/env python3

import logging
import os
import signal
import subprocess
import sys
import requests
import json
import zipfile
import hashlib
import math
from io import BytesIO

# Set up logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')

# Advanced user configs
moniker = "seinode"  # Custom moniker for the node
trust_height_delta = 100000  # Negative height offset for state sync
version_override = False  # Override version fetching. if true, specify version(s) below
p2p_port = 26656
rpc_port = 26657
lcd_port = 1317
grpc_port = 9090
grpc_web_port = 9091
pprof_port = 6060

# Chain binary version ["version_override" must be true to use]
MAINNET_VERSION = "v5.9.0-hotfix"
DEVNET_VERSION = "v5.9.0-hotfix"
TESTNET_VERSION = "v5.9.0-hotfix"

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
This tool will download the seid binary from sei-binaries and wipe any existing state.
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

# Computes the sha256 hash of a file
def compute_sha256(file_path):
    sha256 = hashlib.sha256()
    with open(file_path, "rb") as f:
        for chunk in iter(lambda: f.read(4096), b""):
            sha256.update(chunk)
    return sha256.hexdigest()

# Compile and install release based on version tag
def compile_and_install_release(version):
    logging.info(f"Starting compilation and installation for version: {version}")
    try:
        zip_url = f"https://github.com/sei-protocol/sei-chain/archive/refs/tags/{version}.zip"
        logging.info(f"Constructed zip URL: {zip_url}")

        logging.info("Initiating download of the release zip file...")
        response = requests.get(zip_url, timeout=30)
        response.raise_for_status()
        logging.info("Download completed successfully.")

        logging.info("Extracting the zip file...")
        zip_file = zipfile.ZipFile(BytesIO(response.content))
        zip_file.extractall(".")
        logging.info("Extraction completed.")

        # Get the name of the extracted directory
        extracted_dir = zip_file.namelist()[0].rstrip('/')
        logging.info(f"Extracted directory: {extracted_dir}")

        # Define the new directory name
        new_dir_name = "sei-chain"

        # Check if the new directory name already exists
        if os.path.exists(new_dir_name):
            logging.error(f"The directory '{new_dir_name}' already exists. It will be removed.")
            sys.exit(1)

        # Rename the extracted directory to 'sei-chain'
        logging.info(f"Renaming '{extracted_dir}' to '{new_dir_name}'")
        os.rename(extracted_dir, new_dir_name)
        logging.info(f"Renaming completed.")

        # Change directory to the new directory
        os.chdir(new_dir_name)
        logging.info(f"Changed working directory to '{new_dir_name}'")

        logging.info("Starting the 'make install' process...")
        result = subprocess.run(
            ["make", "install"], 
            check=True, 
            stdout=subprocess.PIPE, 
            stderr=subprocess.PIPE, 
            text=True
        )
        logging.info("Make install completed successfully.")
        logging.debug(f"Make install stdout: {result.stdout}")
        logging.debug(f"Make install stderr: {result.stderr}")

        logging.info(f"Successfully installed version: {version}")
        logging.info(f"Output from make install:\n{result.stdout}")

    except requests.exceptions.HTTPError as e:
        logging.error(f"HTTP error occurred: {e}")
        sys.exit(1)
    except requests.exceptions.RequestException as e:
        logging.error(f"Error downloading files: {e}")
        sys.exit(1)
    except zipfile.BadZipFile:
        logging.error("Error unzipping file. The downloaded file may be corrupt.")
        sys.exit(1)
    except subprocess.CalledProcessError as e:
        # Display the output and error from the make command if it fails
        logging.error(f"Installation failed during 'make install': {e}")
        logging.error(f"Error output: {e.stderr}")
        sys.exit(1)
    except Exception as e:
        logging.error(f"An unexpected error occurred: {e}")
        sys.exit(1)

def install_release(version):
    try:
        base_url = f"https://github.com/alexander-sei/sei-binaries/releases/download/{version}/"
        tag = f"seid-{version}-linux-amd64"
        binary_url = f"{base_url}{tag}"
        sha256_url = f"{base_url}{tag}.sha256"
        install_dir = "/usr/local/bin"
        binary_path = os.path.join(install_dir, "seid")

        logging.info(f"Downloading binary from {binary_url}")
        binary_response = requests.get(binary_url)
        binary_response.raise_for_status()

        logging.info(f"Downloading SHA256 checksum from {sha256_url}")
        sha256_response = requests.get(sha256_url)
        sha256_response.raise_for_status()

        # Extract the SHA256 hash from the .sha256 file
        sha256_hash = sha256_response.text.strip().split()[0]
        logging.info(f"Expected SHA256 hash: {sha256_hash}")

        # Save the binary to a temporary file
        temp_binary_path = "/tmp/seid-binary"
        with open(temp_binary_path, "wb") as binary_file:
            binary_file.write(binary_response.content)

        # Compute the SHA256 hash of the downloaded binary
        logging.info(f"Computing SHA256 hash of the binary at {temp_binary_path}")
        sha256_computed = compute_sha256(temp_binary_path)
        logging.info(f"Computed SHA256 hash: {sha256_computed}")

        # Compare the computed hash with the expected hash
        if sha256_computed != sha256_hash:
            raise ValueError("SHA256 hash mismatch! The binary may be corrupted or tampered with.")
        else:
            logging.info("SHA256 hash verification passed.")

        # Move the binary to the install directory and rename it to 'seid'
        logging.info(f"Moving binary to {binary_path}")
        subprocess.run(["sudo", "mv", temp_binary_path, binary_path], check=True)

        # Make the binary executable
        logging.info(f"Setting executable permissions for {binary_path}")
        subprocess.run(["sudo", "chmod", "+x", binary_path], check=True)

        logging.info(f"Successfully installed 'seid' version: {version} to {binary_path}")

    except requests.HTTPError as http_err:
        logging.error(f"HTTP error occurred: {http_err}")
    except Exception as err:
        logging.error(f"An error occurred: {err}")

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
def get_state_sync_params(rpc_url, trust_height_delta, chain_id):
    response = requests.get(f"{rpc_url}/status")
    latest_height = int(response.json()['sync_info']['latest_block_height'])
    
    # Calculate sync block height
    sync_block_height = latest_height - trust_height_delta if latest_height > trust_height_delta else latest_height
    
    # Determine the rounding based on the chain_id
    if chain_id.lower() == 'pacific-1':
        # Round sync block height to the next 100,000 for mainnet
        rounded_sync_block_height = math.ceil(sync_block_height / 100000) * 100000 + 2
    else:
        # Round sync block height to the next 2,000 for devnet or testnet
        rounded_sync_block_height = math.ceil(sync_block_height / 2000) * 2000 + 2
    
    # Fetch block hash
    response = requests.get(f"{rpc_url}/block?height={rounded_sync_block_height}")
    sync_block_hash = response.json()['block_id']['hash']
    
    return rounded_sync_block_height, sync_block_hash

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

        home_dir = os.path.expanduser('~/.sei')

        if env == "local":
            # Clean up previous data, init seid with given chain ID and moniker
            compile_and_install_release(version)
            
            subprocess.run(["rm", "-rf", home_dir])
            subprocess.run(["seid", "init", moniker, "--chain-id", chain_id], check=True)

            logging.info("Running local initialization script...")
            local_script_path = os.path.expanduser('~/sei-chain/scripts/initialize_local_chain.sh')
            run_command(f"chmod +x {local_script_path}")
            run_command(local_script_path)

        else:
            # Install selected release
            install_release(version)

            # Clean up previous data, init seid with given chain ID and moniker
            subprocess.run(["rm", "-rf", home_dir])
            subprocess.run(["seid", "init", moniker, "--chain-id", chain_id], check=True)

            # Fetch state-sync params and persistent peers
            sync_block_height, sync_block_hash = get_state_sync_params(rpc_url, trust_height_delta,chain_id)
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
                config_data = config_data.replace('fetchers = "4"', 'fetchers = "2"')
                config_data = config_data.replace('send-rate = 20480000', 'send-rate = 20480000000000')
                config_data = config_data.replace('recv-rate = 20480000', 'recv-rate = 20480000000000')
                config_data = config_data.replace('chunk-request-timeout = "15s"', 'chunk-request-timeout = "10s"')
                config_data = config_data.replace('laddr = "tcp://127.0.0.1:26657"', f'laddr = "tcp://127.0.0.1:{rpc_port}"')
                config_data = config_data.replace('laddr = "tcp://0.0.0.0:26656"', f'laddr = "tcp://0.0.0.0:{p2p_port}"')
                config_data = config_data.replace('pprof-laddr = "localhost:6060"', f'pprof-laddr = "localhost:{pprof_port}"')

            with open(config_path, 'w') as file:
                file.write(config_data)
            
            # Read and modify app.toml
            with open(app_config_path, 'r') as file:
                app_data = file.read()
                app_data = app_data.replace('address = "tcp://0.0.0.0:1317"', f'address = "tcp://0.0.0.0:{lcd_port}"')
                app_data = app_data.replace('address = "0.0.0.0:9090"', f'address = "0.0.0.0:{grpc_port}"')
                app_data = app_data.replace('address = "0.0.0.0:9091"', f'address = "0.0.0.0:{grpc_web_port}"')
                if db_choice == "2":
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
