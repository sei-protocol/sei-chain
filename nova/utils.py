import subprocess, json, time

def run(cmd):
    try:
        output = subprocess.check_output(cmd, shell=True, text=True)
        return output
    except subprocess.CalledProcessError as e:
        log(f"❗ Command failed: {e}")
        return None

def log(msg):
    print(msg)
    with open("logs.txt", "a", encoding="utf-8") as f:
        f.write(f"[{time.ctime()}] {msg}\n")

def get_address(wallet):
    out = run(f"seid keys show {wallet} -a")
    return out.strip() if out else ""

def get_balance(address, rpc):
    out = run(f"seid query bank balances {address} --node {rpc} --output json")
    try:
        json_out = json.loads(out)
        return int(json_out["balances"][0]["amount"])
    except:
        return 0
