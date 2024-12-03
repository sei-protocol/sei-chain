import os
import json
import subprocess
import logging
from concurrent.futures import ThreadPoolExecutor, as_completed

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')

def read_files_and_run_command(directory, max_threads=10):
    with ThreadPoolExecutor(max_workers=max_threads) as executor:
        futures = []
        for filename in os.listdir(directory):
            file_path = os.path.join(directory, filename)
            if os.path.isfile(file_path) and filename.endswith('.json'):
                with open(file_path, 'r') as file:
                    data = json.load(file)
                    mnemonic = data.get('mnemonic')
                    if mnemonic:
                        file_prefix = filename.split('.')[0]
                        logging.info(f"Processing file: {filename}")
                        futures.append(executor.submit(run_commands, mnemonic, file_prefix))

        for future in as_completed(futures):
            try:
                future.result()
            except Exception as e:
                logging.error(f"Error occurred: {e}")

def run_commands(mnemonic, file_prefix):
    commands = [
        f"printf '{mnemonic}\n12345678\n' | ~/go/bin/seid keys add {file_prefix} --recover",
        f"printf '12345678\n' | ~/go/bin/seid tx ct init-account usei --from {file_prefix} --fees 20000usei -y -b block",
        f"printf '12345678\n' | ~/go/bin/seid tx ct deposit usei 1000000000 --from {file_prefix} --fees 20000usei -y -b block",
        f"printf '12345678\n' | ~/go/bin/seid tx ct apply-pending-balance usei --from {file_prefix} --fees 20000usei -y",
        f"printf '12345678\n' | ~/go/bin/seid keys delete {file_prefix} -y"
    ]
    for cmd in commands:
        logging.info(f"Running command: {cmd}")
        process = subprocess.Popen(cmd, stdin=subprocess.PIPE, text=True, shell=True, start_new_session=True)
        stdout, stderr = process.communicate()
        if process.returncode == 0:
            logging.info(f"Command succeeded: {cmd}")
        else:
            logging.error(f"Command failed: {cmd}\nError: {stderr}")

if __name__ == "__main__":
    directory = '/root/test_accounts'
    logging.info(f"Starting to process directory: {directory}")
    read_files_and_run_command(directory, max_threads=50)
    logging.info("Finished processing directory")
