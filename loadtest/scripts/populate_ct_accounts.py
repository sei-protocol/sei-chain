import os
import json
import subprocess
from concurrent.futures import ThreadPoolExecutor

def read_files_and_run_command(directory):
    tasks = []
    with ThreadPoolExecutor(max_workers=64) as executor:
        for filename in os.listdir(directory):
            file_path = os.path.join(directory, filename)
            if os.path.isfile(file_path) and filename.endswith('.json'):
                with open(file_path, 'r') as file:
                    data = json.load(file)
                    mnemonic = data.get('mnemonic')
                    if mnemonic:
                        file_prefix = filename.split('.')[0]
                        tasks.append(executor.submit(run_command_with_mnemonic, mnemonic, file_prefix))
        for task in tasks:
            task.result()  # Wait for all tasks to complete

def run_command_with_mnemonic(mnemonic, file_prefix):
    commands = [
        f"printf '{mnemonic}\n12345678\n' | ~/go/bin/seid keys add {file_prefix} --recover",
        f"printf '12345678\n' | ~/go/bin/seid tx ct init-account usei --from {file_prefix} --fees 20000usei -y",
        f"printf '12345678\n' | ~/go/bin/seid tx ct deposit usei 1000000000 --from {file_prefix} --fees 20000usei -y",
        f"printf '12345678\n' | ~/go/bin/seid tx ct apply-pending-balance usei --from {file_prefix} --fees 20000usei -y",
        f"printf '12345678\n' | ~/go/bin/seid keys delete {file_prefix} -y"
    ]
    for cmd in commands:
        process = subprocess.Popen(cmd, stdin=subprocess.PIPE, text=True, shell=True)
        process.communicate()

if __name__ == "__main__":
    directory = '/root/test_accounts'
    read_files_and_run_command(directory)