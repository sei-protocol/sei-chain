import json
import sys
import argparse
from functools import partial

def build_counter_deps(base_filepath, output_filepath, code_id, contract_address):
    with open(base_filepath, 'r') as file:
        proposal = json.load(file)
        proposal["wasm_dependency_mapping"]["contract_address"] = contract_address
        for access_op in proposal["wasm_dependency_mapping"]["base_access_ops"]:
            selector_type = access_op["selector_type"]
            resource_type = access_op["operation"]["resource_type"]
            id_template = access_op["operation"]["identifier_template"]
            selector = access_op.get("selector")
            if selector_type == "CONTRACT_ADDRESS":
                if "contract_address" in selector:
                    access_op["selector"] = contract_address
            elif resource_type == "KV_WASM_CODE":
                if "contract_code_id" in id_template:
                    access_op["operation"]["identifier_template"] = f"01{int(code_id):016x}"
            elif resource_type == "KV_WASM_PINNED_CODE_INDEX":
                if "contract_code_id" in id_template:
                    access_op["operation"]["identifier_template"] = f"07{int(code_id):016x}"
        with open(output_filepath, 'w') as output_file:
            json.dump(proposal, output_file, indent=4)

def main():
    parser = argparse.ArgumentParser(description="This is a parser to generate a parallel dependency json file from a template")
    parser.add_argument('Action', type=str, help="The action to perform (eg. build_counter_deps)")
    parser.add_argument('--base-filepath', dest="base_filepath", type=str, help="The json template filepath")
    parser.add_argument('--output-filepath', dest="output_filepath", type=str, help="The json dependency output filepath")
    parser.add_argument('--code-id', type=int, dest="code_id", help="The code id for which to generate dependencies")
    parser.add_argument('--contract-address', dest="contract_address", type=str, help="The contract address for which to generate dependencies")
    args = parser.parse_args()
    actions = {
        "build_counter_deps": partial(build_counter_deps, args.base_filepath, args.output_filepath, args.code_id, args.contract_address),
    }
    if args.Action not in actions:
        print("Invalid Action")
    actions[args.Action]()


if __name__ == "__main__":
    main()
