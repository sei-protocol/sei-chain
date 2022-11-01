import json
import os
import subprocess
import sys
import threading
import time

PARALLEISM=32

# Global Variable used for accounts
# Does not need to be thread safe, each thread should only be writing to its own index
global_accounts_mapping = {}

def add_key(account_name, local=False):
    if local:
        add_key_cmd = f"yes | ~/go/bin/seid keys add {account_name} --keyring-backend test"
    else:
        add_key_cmd = f"printf '12345678\n' | ~/go/bin/seid keys add {account_name}"
    add_key_output = subprocess.check_output(
        [add_key_cmd],
        stderr=subprocess.STDOUT,
        shell=True,
    ).decode()
    splitted_outputs = add_key_output.split('\n')
    address = splitted_outputs[3].split(': ')[1]
    mnemonic = splitted_outputs[11]
    return address, mnemonic


def add_account(account_name, address, mnemonic, local=False):
    if local:
        add_account_cmd = f"~/go/bin/seid add-genesis-account {address} 1000000000usei --keyring-backend test"
    else:
        add_account_cmd = f"printf '12345678\n' | ~/go/bin/seid add-genesis-account {address} 1000000000usei"

    home_path = os.path.expanduser('~')
    filename = f"{home_path}/test_accounts/{account_name}.json"
    os.makedirs(os.path.dirname(filename), exist_ok=True)
    with open(filename, 'w') as f:
        data = {
            "address": address,
            "mnemonic": mnemonic,
        }
        json.dump(data, f)

    return add_account_cmd


def create_genesis_account(account_index, account_name, local=False):
    address, mnemonic = add_key(account_name=account_name, local=local)
    add_account_cmd = add_account(account_name=account_name, address=address, mnemonic=mnemonic, local=local)

    retry_counter = 0
    sleep_time = 1

    while True and retry_counter < 1000:
        try:
            print(f'Running: ${add_account_cmd}')
            subprocess.call(
                [add_account_cmd],
                shell=True,
                timeout=20,
            )
            break
        except subprocess.CalledProcessError as e:
            print(f"Encountered error {e}, retried {retry_counter} times")
            retry_counter += 1
            sleep_time += 0.5
            time.sleep(sleep_time)

    if retry_counter >= 1000:
        exit(-1)

    global_accounts_mapping[account_index] = {
        "balance": {
            "address": address,
            "coins": [
                {
                    "denom": "usei",
                    "amount": "1000000000"
                }
            ]
        },
        "account": {
          "@type": "/cosmos.auth.v1beta1.BaseAccount",
          "address": address,
          "pub_key": None,
          "account_number": "0",
          "sequence": "0"
        }
    }


def bulk_create_genesis_accounts(number_of_accounts, start_idx, is_local=False):
    for i in range(start_idx, start_idx + number_of_accounts):
        create_genesis_account(i, f"ta{i}", is_local)
        print(f"Created account {i}")


def read_genesis_file():
    with open("/root/.sei/config/genesis.json", 'r') as f:
        return json.load(f)


def write_genesis_file(data):
    print("Writing results to genesis file")
    with open("/root/.sei/config/genesis.json", 'w') as f:
        json.dump(data, f, indent=4)


def main():
    args = sys.argv[1:]
    number_of_accounts = int(args[0])
    is_local = False
    if len(args) > 1 and args[1] == "loc":
        is_local = True

    genesis_file = read_genesis_file()

    num_threads = number_of_accounts // PARALLEISM
    threads = []
    for i in range(0, number_of_accounts, num_threads):
        threads.append(threading.Thread(target=bulk_create_genesis_accounts, args=(num_threads, i, is_local)))

    print("Starting threads account")
    for t in threads:
        t.start()

    print("Waiting for threads")
    for t in threads:
        t.join()

    sorted_keys = sorted(list(global_accounts_mapping.keys()))
    account_info = [0] * len(sorted_keys)
    balances = [0] * len(sorted_keys)
    for key in sorted_keys:
        balances[key] = global_accounts_mapping[key]["balance"]
        account_info[key] = global_accounts_mapping[key]["account"]

    genesis_file["app_state"]["bank"]["balances"] = genesis_file["app_state"]["bank"]["balances"] + balances
    genesis_file["app_state"]["auth"]["accounts"] = genesis_file["app_state"]["auth"]["accounts"] + account_info

    num_accounts_created = len([account for account in account_info if account != 0])
    print(f'Created {num_accounts_created} accounts')

    assert num_accounts_created >= number_of_accounts
    write_genesis_file(genesis_file)

if __name__ == "__main__":
    main()
