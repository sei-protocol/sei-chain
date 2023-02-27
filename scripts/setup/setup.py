import argparse
import logging
import json
import os
import subprocess

from datetime import datetime
from getpass import getpass
from shutil import copytree


logging.basicConfig(
    level=logging.INFO,
    encoding='utf-8',
    format='%(asctime)s::%(levelname)s:: %(message)s',
    datefmt='%m/%d/%Y %I:%M:%S %p'
)
logging.getLogger().setLevel(logging.INFO)

HOME_PATH = os.path.expanduser('~')

SEI_ROOT_DIR = f'{HOME_PATH}/.sei'
SEI_CONFIG_DIR = f'{SEI_ROOT_DIR}/config'
SEI_CONFIG_TOML_PATH = f'{SEI_CONFIG_DIR}/config.toml'

PREPARE_GENESIS = "prepare-genesis"
SETUP_VALIDATOR = "setup-validator"
SETUP_PRICE_FEEDER = "setup-price-feeder"

DEFAULT_VALIDATOR_ACC_NAME = 'admin'
ORACLE_PRICE_FEEDER_ACC_NAME = 'oracle-price-feeder'

account_cache = {}
class Account:
    """Account information """
    def __init__(self, account_name, address, mnemonic, password) -> None:
        self.account_name = account_name
        self.address = address
        self.mnemonic = mnemonic
        self.password = password


def run_command(command):
    """Run a command and return the output."""
    try:
        output = subprocess.check_output(command, shell=True, stderr=subprocess.STDOUT)
        return output.decode().strip()
    except subprocess.CalledProcessError as err:
        error_msg = f"Error running command '{command}': \n {err.output.decode()}"
        raise RuntimeError(error_msg) from err

def run_with_password(command, password):
    """Run a command with a password."""
    return run_command(f"printf '{password}\\n{password}\\n' | {command}")

def get_git_root_dir():
    """Get the root directory of the git repository."""
    git_root_dir = run_command('git rev-parse --show-toplevel')
    return git_root_dir


def set_git_root_as_current_working_dir():
    """Set the current working directory to the root of the git repository."""
    git_root_dir = get_git_root_dir()
    os.chdir(git_root_dir)
    logging.info('Current working directory: %s', os.getcwd())


def validate_clean_state():
    """Validate that the current working directory is clean."""
    if os.path.isfile(SEI_CONFIG_TOML_PATH):
        raise RuntimeError(f'The file {SEI_CONFIG_TOML_PATH} already exists. Please reset your {SEI_ROOT_DIR} state.')
    logging.info('Validated clean state.')

    logging.info('Updating seid binary...')
    run_command('make install')
    logging.info('make install successful.')


def validate_version(version):
    """Validate that the version of the SEI blockchain software is correct."""
    version_json_output = json.loads(run_command('seid version --long --output json'))
    if version_json_output['version'] != version:
        raise RuntimeError(f'Expected version {version} but got {version_json_output["version"]}')

def install_price_feeder():
    """Make the oracle binary."""
    logging.info('Making oracle binary...')
    run_command('make install price-feeder')
    logging.info('Made oracle binary.')


def set_price_feeder():
    """Set the price feeder."""
    logging.info('Setting price feeder...')
    addr, _ = seid_add_key(ORACLE_PRICE_FEEDER_ACC_NAME)
    run_with_password(
        f'seid tx oracle set-feeder $(seid keys show {ORACLE_PRICE_FEEDER_ACC_NAME} -a) --from admin --yes --fees=2000usei',
        account_cache[ORACLE_PRICE_FEEDER_ACC_NAME].password
    )
    logging.info("Please send sei tokens to the feeder account '%s' to fund it", addr)


def output_price_feeder_config(chain_id):
    config_path = f'{SEI_ROOT_DIR}/oracle-price-feeder.toml'

    with open('./oracle/price-feeder/config.example.toml', 'r', encoding='utf8') as f:
        config = f.read()

    key_password = getpass('Please enter a password for the validator account key: \n')
    val_addr = json.loads(run_with_password(f'seid keys show {DEFAULT_VALIDATOR_ACC_NAME} --bech=val --output json', key_password))['address']

    config = config.replace('<FEEDER_ADDR>', account_cache[ORACLE_PRICE_FEEDER_ACC_NAME].address)
    config = config.replace('<CHAIN_ID>', chain_id)
    config = config.replace('<VALIDATOR_ADDR>', val_addr)

    with open(config_path, 'w+', encoding='utf8') as f:
        f.write(config)

    logging.info('Price feeder config file created at %s', config_path)

def cleanup_sei():
    """Cleanup the SEI state."""
    if os.path.exists(SEI_ROOT_DIR):
        backup_file = f'{SEI_ROOT_DIR}_backup_{datetime.now().strftime("%Y%m%d_%H%M%S")}'
        copytree(f'{SEI_ROOT_DIR}', backup_file)
        logging.info('Backed up SEI state to %s', backup_file)
    run_command(f'rm -rf {SEI_ROOT_DIR}')
    logging.info('Removed %s directory.', SEI_ROOT_DIR)

def init_sei(chain_id, moniker):
    """Initialize the SEI blockchain."""
    logging.info('Initializing SEI blockchain...')
    run_command(f'seid init {moniker} --chain-id {chain_id}')
    logging.info('Initialized SEI blockchain.')


def save_content_to_file(content, file_path):
    """Save content to a file."""
    with open(file_path, 'w+', encoding='utf8') as f:
        f.write(content)


def try_seid_delete_key(account_name, key_password):
    try:
        run_with_password(f'seid keys delete {account_name} -y', key_password)
        logging.info("Deleted existing key if it exists.")
    except Exception:
        logging.info("No existing key found.")


def seid_add_key(account_name):
    """Add a key to the SEI blockchain."""
    key_password = getpass(f'Please enter a password for the account={account_name}: \n')
    try_seid_delete_key(account_name, key_password)
    logging.info("Deleted existing key if it exists.")

    add_key_output = run_with_password(f'seid keys add {account_name} --output json', key_password)

    json_output = json.loads(add_key_output)
    address = json_output['address']
    mnemonic = json_output['mnemonic']

    logging.info('Added genesis account %s with address %s', account_name, address)

    # Cache the account info used to gentx later
    account_cache[account_name] = Account(account_name, address, mnemonic, key_password)
    save_content_to_file(json.dumps(json_output, indent=4), f'{SEI_CONFIG_DIR}/{account_name}_key_info.txt')
    logging.info('Saved key info to %s', f'{SEI_CONFIG_DIR}/{account_name}_key_info.txt')

    return address, mnemonic

def add_genesis_account(account_name, starting_balance):
    """Add a genesis account to the SEI blockchain."""
    address = account_cache[account_name].address
    run_command(f'seid add-genesis-account {address} {starting_balance}')
    logging.info('Added genesis account %s with address %s', account_name, address)
    return address


def gentx(chain_id, account_name, starting_delegation, gentx_args):
    """Generate a gentx for the validator node."""
    account = account_cache[account_name]
    output = run_with_password(f'seid gentx {account.account_name} {starting_delegation} --chain-id={chain_id} {gentx_args}', account.password)
    logging.info(output)

def setup_validator(args):
    """Setup the validator node."""
    if not args.chain_id:
        raise RuntimeError('Please specify a chain ID')
    if not args.moniker:
        raise RuntimeError('Please specify a version')
    cleanup_sei()
    set_git_root_as_current_working_dir()
    validate_clean_state()
    init_sei(args.chain_id, args.moniker)
    seid_add_key(DEFAULT_VALIDATOR_ACC_NAME)

def prepare_genesis(args):
    """Prepare the genesis file."""
    if not args.chain_id:
        raise RuntimeError('Please specify a chain ID')
    if not args.moniker:
        raise RuntimeError('Please specify a version')

    add_genesis_account(DEFAULT_VALIDATOR_ACC_NAME, '12sei')
    gentx(args.chain_id, DEFAULT_VALIDATOR_ACC_NAME, '10sei', args.gentx_args)


def setup_oracle(args):
    if not args.chain_id:
        raise RuntimeError('Please specify a chain ID')

    install_price_feeder()
    set_price_feeder()
    output_price_feeder_config(args.chain_id)


def run():
    """
    Run the command line tool. See README.md for more details.
    """
    parser = argparse.ArgumentParser(description='Command line tool for specifying chain information')
    parser.add_argument('action', type=str, help='Action to preform', choices=[SETUP_VALIDATOR, PREPARE_GENESIS, SETUP_PRICE_FEEDER])
    parser.add_argument('--chain-id', type=str, help='ID of the blockchain network', required=False)
    parser.add_argument('--moniker', type=str, help='Moniker of the validator node', required=False)
    parser.add_argument('--version', type=str, help='Version of the blockchain software')
    parser.add_argument('--gentx-args', type=str, help="args to pass to the gentx call e.g '--ip seinetwork.io --port 123'", required=False, default='')

    # setup-price-feeder
    parser.add_argument('--feeder-addr', type=str, help="Wallet address of the oracle feeder account", required=False)

    args = parser.parse_args()
    logging.info('Chain ID: %s', args.chain_id)
    logging.info('Version: %s', args.version)
    logging.info('Moniker: %s', args.moniker)

    try:
        if args.action in {SETUP_VALIDATOR, PREPARE_GENESIS}:
            setup_validator(args)
            run_command(f"sed -i -e 's/mode = \"full\"/mode = \"validator\"/' {SEI_CONFIG_DIR}/config.toml")

        if args.action == PREPARE_GENESIS:
            prepare_genesis(args)
        elif args.action == SETUP_PRICE_FEEDER:
            setup_oracle(args)

    except RuntimeError as err:
        logging.error("Unable to run %s due to \n: %s", args.action, err)

    # Always validate that the required argument version, is the correct
    validate_version(args.version)

if __name__ == '__main__':
    run()
