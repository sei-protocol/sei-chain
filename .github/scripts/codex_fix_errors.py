#!/usr/bin/env python3
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]

def fix_msgs_go(path):
    text = path.read_text().splitlines()
    if not text[-1].strip().endswith("}"):
        print(f"[+] Fixing missing closing brace in {path}")
        text.append("}")
        path.write_text("\n".join(text))

def fix_tx_go(path):
    text = path.read_text().splitlines()
    fixed = []
    inside_cmd = False
    for i, line in enumerate(text):
        if re.match(r'^\s*RunE:\s*{', line):
            print(f"[+] Fixing stray RunE block at line {i+1} in {path}")
            fixed.append("RunE: func(cmd *cobra.Command, args []string) error {")
            inside_cmd = True
        elif inside_cmd and line.strip() == "},":
            fixed.append("return nil")
            fixed.append("},")
            inside_cmd = False
        else:
            fixed.append(line)
    path.write_text("\n".join(fixed))

def run_go_mod_tidy():
    print("[+] Running go mod tidy to regenerate go.sum")
    subprocess.run(["go", "mod", "tidy"], cwd=ROOT)

def main():
    targets = [
        ROOT / "x/seinet/types/msgs.go",
        ROOT / "x/evm/client/cli/tx.go",
    ]
    for t in targets:
        if t.exists():
            if "msgs.go" in str(t):
                fix_msgs_go(t)
            if "tx.go" in str(t):
                fix_tx_go(t)
    run_go_mod_tidy()

if __name__ == "__main__":
    main()
