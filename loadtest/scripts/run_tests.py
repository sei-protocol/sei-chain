import argparse
import json
import os
import subprocess
import tempfile

from dataclasses import dataclass


# This strategy is used to simulate sudden spikes or bursts of load on the on the chain.
# Burst load testing is done to ensure that the chain hub is able to handle such sudden
# spikes in traffic.
BURST = 'BURST'

#  This is used to simulate a "steady-state" scenario where there is a constant load.
STEADY = 'STEADY'


@dataclass
class LoadTestConfig:
    config_file_path: str
    loadtest_binary_file_path: str


def write_to_temp_json_file(data):
    temp_file = tempfile.NamedTemporaryFile(mode='w', encoding='utf-8', delete=False)
    json.dump(data, temp_file, ensure_ascii=False)
    return temp_file

def create_burst_loadtest_config(base_config_json):
    new_config = base_config_json.copy()
    new_config["constant"] = True
    new_config["metrics_port"] = 9697
    new_config["txs_per_block"] = 2000
    # Run every 20 mins
    new_config["loadtest_interval"] = 1200
    return new_config


def create_steady_loadtest_config(base_config_json):
    new_config = base_config_json.copy()
    new_config["constant"] = True
    new_config["metrics_port"] = 9696
    new_config["txs_per_block"] = 100
    # Run every min
    new_config["loadtest_interval"] = 60
    return new_config


def read_config_json(config_json_file_path):
    # Default path for running on EC2 instances
    file_path = "/home/ubuntu/sei-chain/loadtest/config.json"

    if config_json_file_path is not None:
        file_path = config_json_file_path

    with open(file_path, 'r', encoding="utf-8") as file:
        return json.load(file)


def run_go_loadtest_client(config_file_path, binary_path):
    # Default path for running on EC2 instances
    cmd = ["/home/ubuntu/sei-chain/build/loadtest", "-config-file", config_file_path]
    if binary_path is not None:
       cmd[0] = binary_path

    print(f'Running {" ".join(cmd)}')
    subprocess.run(cmd, check=True)

def run_test(test_type, loadtest_config):
    config = base_config_json = read_config_json(loadtest_config.config_file_path)
    if test_type == BURST:
        config = create_burst_loadtest_config(base_config_json)
    elif test_type == STEADY:
        config = create_steady_loadtest_config(base_config_json)

    temp_file = write_to_temp_json_file(config)
    try:
        run_go_loadtest_client(temp_file.name, binary_path=loadtest_config.loadtest_binary_file_path)
    finally:
        temp_file.close()

def run():
    parser = argparse.ArgumentParser(
                        prog = 'Loadtest Client',
                        description = 'Wrapper for the golang client to run loadtests with different configs')
    parser.add_argument(
        'type',
        help='Type of loadtest to run (e.g constant, burst)',
        type = lambda s : s.upper(),
        choices=[BURST, STEADY],
    )
    parser.add_argument(
        '--config-file',
        help='Base config file to modify',
        required=False,
    )
    parser.add_argument(
        '--loadtest-binary',
        help='binary of the loadtest client to run',
        required=False,
    )

    args = parser.parse_args()
    test_type = args.type
    print(f'type={test_type} loadtests')

    run_test(
        test_type=test_type,
        loadtest_config=LoadTestConfig(args.config_file, args.loadtest_binary)
    )


if __name__ == '__main__':
    run()
