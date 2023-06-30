import json
import sys

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
    return output_filepath

def main():
    args = sys.argv[1:]
    if args[0] == "build_counter_deps":
        print(build_counter_deps(args[1], args[2], args[3], args[4]))
    else:
        print("Unknown args")

if __name__ == "__main__":
    main()
