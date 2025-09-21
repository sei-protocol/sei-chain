UNLICENSED

All rights reserved. This software and its source code may not be copied, modified,
distributed, or used without prior written permission from the authors.

#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Circle CCTP bridge helper."""

import argparse
import json
from urllib import request

CCTP_API_BASE = "https://api.circle.com/v1/cctp"


def burn_usdc(api_key: str, source_chain: str, tx_hash: str, amount: float) -> dict:
    url = f"{CCTP_API_BASE}/burns"
    payload = {
        "sourceChain": source_chain,
        "transactionHash": tx_hash,
        "amount": amount,
    }
    headers = {
        "Authorization": f"Bearer {api_key}",
        "Content-Type": "application/json",
    }
    data = json.dumps(payload).encode()
    req = request.Request(url, data=data, headers=headers, method="POST")
    with request.urlopen(req, timeout=30) as resp:
        return json.loads(resp.read().decode())


def mint_usdc(api_key: str, destination_chain: str, burn_tx_id: str) -> dict:
    url = f"{CCTP_API_BASE}/mints"
    payload = {
        "destinationChain": destination_chain,
        "burnTxId": burn_tx_id,
    }
    headers = {
        "Authorization": f"Bearer {api_key}",
        "Content-Type": "application/json",
    }
    data = json.dumps(payload).encode()
    req = request.Request(url, data=data, headers=headers, method="POST")
    with request.urlopen(req, timeout=30) as resp:
        return json.loads(resp.read().decode())


def transfer(api_key: str, from_chain: str, to_chain: str, tx_hash: str, amount: float) -> tuple[dict, dict, dict]:
    burn = burn_usdc(api_key, from_chain, tx_hash, amount)
    mint = mint_usdc(api_key, to_chain, burn.get("burnTxId"))
    receipt = {
        "source_chain": from_chain,
        "destination_chain": to_chain,
        "burn_tx": burn.get("burnTxId"),
        "mint_tx": mint.get("mintTxId"),
        "amount": amount,
        "x402": f"x402-receipt-{mint.get('mintTxId')}",
    }
    return burn, mint, receipt


def main():
    parser = argparse.ArgumentParser(description="Circle CCTP bridge helper")
    parser.add_argument("--from-chain", required=True, help="source chain ID")
    parser.add_argument("--to-chain", required=True, help="destination chain ID")
    parser.add_argument("--tx-hash", required=True, help="source chain transaction hash")
    parser.add_argument("--amount", required=True, type=float, help="USDC amount to transfer")
    parser.add_argument("--api-key", required=True, help="Circle API key")
    args = parser.parse_args()

    burn, mint, receipt = transfer(
        args.api_key, args.from_chain, args.to_chain, args.tx_hash, args.amount
    )
    print("Burn:", burn)
    print("Mint:", mint)
    print("Receipt:", receipt)


if __name__ == "__main__":
    main()
