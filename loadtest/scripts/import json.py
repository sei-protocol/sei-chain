import json
import os
import subprocess
import sys
import time


# Global Variable used for accounts
# Does not need to be thread safe, each thread should only be writing to its own index
global_accounts_mapping = {}
home_path = os.path.expanduser('~')

def add_key(account_name):
    add_key_cmd = f"printf '12345678\n' | ~/go/bin/seid keys add {account_name} --keyring-backend test"
    add_key_output = subprocess.check_output(
        [add_key_cmd],
        stderr=subprocess.STDOUT,
        shell=True,
    ).decode()

    splitted_outputs = add_key_output.split('\n')
    address = splitted_outputs[3].split(': ')[1]
    mnemonic = splitted_outputs[11]

    filename = f"{home_path}/test_accounts/{account_name}.json"
    os.makedirs(os.path.dirname(filename), exist_ok=True)
    with open(filename, 'w') as file:
        data = {
            "address": address,
            "mnemonic": mnemonic,
        }
        json.dump(data, file)

    return address, mnemonic


def get_bank_send_cmd(from_address, to_address):
    add_account_cmd = f"printf '12345678\n' | seid tx bank send {from_address} {to_address}  100000000000usei --gas 2000000 --fees 100000usei --chain-id sei-devnet-3-internal --broadcast-mode block --yes"
    return add_account_cmd


def create_account(account_name, from_address):
    address, _ = add_key(account_name=account_name)
    send_bank_balance_cmd = get_bank_send_cmd(from_address=from_address, to_address=address)

    retry_counter = 0
    sleep_time = 1

    while True and retry_counter < 1000:
        try:
            print(f'Running: ${send_bank_balance_cmd}')
            subprocess.call(
                [send_bank_balance_cmd],
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


def main():
    args = sys.argv[1:]
    number_of_accounts = int(args[0])
    from_address = args[1]
    for i in range(0, number_of_accounts, ):
        account_name =  f'ta{i}'
        create_account(account_name, from_address)

    print(f'Created {number_of_accounts} accounts')

if __name__ == "__main__":
    main()
