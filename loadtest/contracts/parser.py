import json
import sys

def get_code_id(raw_response):
    response = json.loads(raw_response)
    log = json.loads(response["raw_log"].replace("\\",""))[0]
    for event in log["events"]:
        if event["type"] == "store_code":
            for attribute in event["attributes"]:
                if attribute["key"] == "code_id":
                    return int(attribute["value"])
    return -1

def get_contract_address(raw_response):
    response = json.loads(raw_response)
    log = response["logs"][0]
    for event in log["events"]:
        if event["type"] == "instantiate":
            for attribute in event["attributes"]:
                if attribute["key"] == "_contract_address":
                    return attribute["value"]
    return ""

def get_proposal_id(raw_response):
    response = json.loads(raw_response)
    log = response["logs"][0]
    for event in log["events"]:
        if event["type"] == "submit_proposal":
            for attribute in event["attributes"]:
                if attribute["key"] == "proposal_id":
                    return int(attribute["value"])
    return -1

def main():
    args = sys.argv[1:]
    if args[0] == "code_id":
        print(get_code_id(args[1]))
    elif args[0] == "contract_address":
        print(get_contract_address(args[1]))
    elif args[0] == "proposal_id":
        print(get_proposal_id(args[1]))
    else:
        print("Unknown args")

if __name__ == "__main__":
    main()
