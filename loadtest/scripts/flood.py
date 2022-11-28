import os
import subprocess

# Global Variable used for accounts
# Does not need to be thread safe, each thread should only be writing to its own index
global_accounts_mapping = {}
home_path = os.path.expanduser('~')

def send_orders(price_start, price_end, cnt):
    step = 1.0 * (price_end - price_start) / cnt
    orders = [f"Long?{p * step + price_start}?1?SEI?ATOM?LIMIT?'{{}}'" for p in range(cnt)]
    orders_str = " ".join(orders)
    cmd = f"printf \"12345678\n\" | ~/go/bin/seid tx dex place-orders sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m {orders_str} --amount=10000usei -y --from=admin --chain-id=sei-loadtest-testnet --fees=0usei --gas=0 --broadcast-mode=block"
    subprocess.call(
        [cmd],
        stderr=subprocess.STDOUT,
        shell=True,
    )

def main():
    for i in range(4000):
        send_orders(i * 1000 + 1, (i + 1) * 1000 + 1, 1000)

if __name__ == "__main__":
    main()
