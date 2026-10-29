#!/usr/bin/env python3
"""Writes the CLOB bench manifest with shardRouting set from a running chain's committee.

Usage: add_shard_routing.py <seiload-clob.yaml> <out.yaml> <autobahn.json>

autobahn.json is the committee file of any validator of the chain, for example:
  kubectl -n <namespace> exec <chain-id>-0-0 -c seid -- cat /home/nonroot/.sei/config/autobahn.json
Validator keys change with every genesis, so the routing is regenerated for each new chain.
"""
import json
import sys


def main() -> None:
    src, dst, committee = sys.argv[1:4]
    manifest = open(src).read()
    validators = json.load(open(committee))["validators"]
    routing = {
        "validators": [
            {
                "consensusPubKey": v["validator_key"],
                "power": 1,
                "endpoint": "http://" + v["address"].rsplit(":", 1)[0] + ":8545",
            }
            for v in validators
        ]
    }
    start = manifest.index("profile.json: |\n") + len("profile.json: |\n")
    end = manifest.index("\n---", start)
    profile = json.loads(manifest[start:end])
    profile["shardRouting"] = routing
    body = "\n".join("    " + line for line in json.dumps(profile, indent=2).splitlines())
    open(dst, "w").write(manifest[:start] + body + manifest[end:])


if __name__ == "__main__":
    main()
