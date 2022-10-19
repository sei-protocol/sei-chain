import json
import os
import subprocess
import sys
import threading
import time

PARALLEISM=32

def add_genesis_account(account_name, lock, local=False):
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
    success = False
    retry_counter = 5
    sleep_time = 1
    while not success and retry_counter > 0:
        try:
            with lock:
                subprocess.check_call(
                    [add_account_cmd],
                    shell=True,
                    timeout=20,
                )
                success = True
        except subprocess.CalledProcessError as e:
            print(f"Encountered error {e}, retrying {retry_counter - 1} times")
            retry_counter -= 1
            sleep_time += 0.5
            time.sleep(sleep_time)


def bulk_create_genesis_accounts(number_of_accounts, start_idx, lock, is_local=False):
    for i in range(start_idx, start_idx + number_of_accounts):
        print(f"Creating account {i}")
        add_genesis_account(f"ta{i}", lock, is_local)

def main():
    args = sys.argv[1:]
    number_of_accounts = int(args[0])
    is_local = False
    if len(args) > 1 and args[1] == "loc":
        is_local = True
    num_threads = number_of_accounts // PARALLEISM
    threads = []
    lock=threading.Lock()
    for i in range(0, number_of_accounts, num_threads):
        threads.append(threading.Thread(target=bulk_create_genesis_accounts, args=(num_threads, i, lock, is_local)))
    for t in threads:
        t.start()
    for t in threads:
        t.join()

if __name__ == "__main__":
    main()
