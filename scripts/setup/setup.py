import argparse
import logging
import os
import subprocess

logging.basicConfig(
    level=logging.INFO,
    encoding='utf-8',
    format='%(asctime)s::%(levelname)s:: %(message)s',
    datefmt='%m/%d/%Y %I:%M:%S %p'
)
logging.getLogger().setLevel(logging.INFO)

SEI_ROOT_DIR = "~/.sei"
SEI_CONFIG_DIR = f"{SEI_ROOT_DIR}/config"
SEI_CONFIG_TOML_PATH = f"{SEI_CONFIG_DIR}/config.toml"


def run_command(command):
    """Run a command and return the output."""
    try:
        output = subprocess.check_output(command, shell=True, stderr=subprocess.STDOUT)
        return output.decode().strip()
    except subprocess.CalledProcessError as err:
        command = " ".join(err.cmd)
        error_msg = f"Error running command '{command}'"
        raise RuntimeError(error_msg) from err


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


def run():
    """Run the setup script."""
    parser = argparse.ArgumentParser(description='Command line tool for specifying chain information')
    parser.add_argument('--chain-id', type=str, help='ID of the blockchain network')
    parser.add_argument('--version', type=str, help='Version of the blockchain software')
    parser.add_argument('--moniker', type=str, help='Moniker of the validator node')
    parser.add_argument('--p2p-endpoint', type=str, help='P2P endpoint of the validator node', required=False)
    parser.add_argument('--skip-validation', type=bool, help='Skip validation of the state of this machine', default=False)

    args = parser.parse_args()
    logging.info('Chain ID: %s', args.chain_id)
    logging.info('Version: %s', args.version)
    logging.info('Moniker: %s', args.moniker)
    logging.info('P2P Endpoint: %s', args.p2p_endpoint)

    set_git_root_as_current_working_dir()
    if not args.skip_validation:
        validate_clean_state()


if __name__ == '__main__':
    run()
