import argparse
import re
import subprocess
import yaml


class TestRunner:

    def __init__(self):
        pass

    def load_yaml_file(self, filepath):
        with open(filepath, 'r') as f:
            data = yaml.safe_load(f)
        return data

    # Function to process YAML
    def process_data(self, data):
        for test in data:
            self.run_test(test)

    # Function to execute a single test case
    def run_test(self, test):
        test_name = test["name"]
        print("\n========== " + test_name + " =========", flush=True)
        inputs = test["inputs"]
        env_map = {}
        for input in inputs:
            cmd = input['cmd']
            container = input.get("node", "c28e1910a0ce")
            print(f'Input : {cmd}', flush=True)
            output = self.run_bash_command(cmd, True, container, env_map, False)
            if input.get('env'):
                env_map[input['env']] = output
            result = output
            print(f'Output: {result}', flush=True)
        for verifier in test["verifiers"]:
            if not self.verify_result(env_map, verifier):
                print("Test failed for {}".format(verifier), flush=True)
                exit(1)
        print("Test Passed", flush=True)

    # Function to verify the result of a single test case
    def verify_result(self, env_map, verifier):
        type = verifier["type"]
        if type == "eval":
            elems = verifier["expr"].strip().split()
            expr = ""
            for i in range(len(elems)):
                if elems[i] in env_map:
                    variable = env_map[elems[i]]
                    if str(variable).isnumeric():
                        expr = expr + f' {variable}'
                    else:
                        expr = expr + f' "{variable}"'
                else:
                    expr += f' {elems[i]}'
            print(f'Evaluating: {expr}')
            return eval(expr)
        elif type == "regex":
            env_key = verifier["result"]
            expression = str(verifier["expr"])
            result = env_map[env_key].strip()
            return re.match(expression, result)
        else:
            return False

    # Helper function to execute a single command within the docker container
    def run_bash_command(self, command, docker=False, container="", env_map=None, verbose=False):
        if docker:
            envs = ""
            if env_map:
                for key in env_map:
                    envs += f'-e {key}=\'{env_map[key]}\' '
            full_cmd = f'docker exec {envs} {container} /bin/bash -c \'export PATH=$PATH:/root/go/bin:/root/.foundry/bin && {command}\''
        else:
            full_cmd = command
        if verbose:
            print(f'Command: {full_cmd}')
        process = subprocess.Popen(full_cmd, stdout=subprocess.PIPE, shell=True)
        output, error = process.communicate()
        return output.decode().strip()


def main():
    parser = argparse.ArgumentParser(
        description='Integration test runner',
        formatter_class=argparse.ArgumentDefaultsHelpFormatter,
    )
    parser.add_argument('filepath', type=str, help='The path to the JSON file')
    args = parser.parse_args()
    runner = TestRunner()
    data = runner.load_yaml_file(args.filepath)
    runner.process_data(data)


if __name__ == '__main__':
    main()
