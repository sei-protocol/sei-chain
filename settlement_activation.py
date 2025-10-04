import os
from web3 import Web3

# Load private key securely from environment
private_key = os.getenv("SETTLEMENT_KEY")
if not private_key:
    raise Exception("❌ SETTLEMENT_KEY not set in environment")

# Setup RPC
w3 = Web3(Web3.HTTPProvider("https://ethereum.publicnode.com"))

# Sender (must own the USDC or be approved)
sender = w3.eth.account.from_key(private_key)
print("🔑 Using sender address:", sender.address)

# Confirm sender is the correct one
EXPECTED_SENDER = "0xb2b297eF9449aa0905bC318B3bd258c4804BAd98"
if sender.address.lower() != EXPECTED_SENDER.lower():
    raise Exception(f"❌ Incorrect key: expected {EXPECTED_SENDER}, got {sender.address}")

# Recipient: EE7
recipient = "0x996994d2914df4eee6176fd5ee152e2922787ee7"

# Settlement contract
contract_address = "0xd973555aAaa8d50a84d93D15dAc02ABE5c4D00c1"

# ABI (minimal for settle)
abi = [
  {
    "inputs": [
      {"internalType": "address","name": "to","type": "address"},
      {"internalType": "uint256","name": "amount","type": "uint256"}
    ],
    "name": "settle",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  }
]

contract = w3.eth.contract(address=contract_address, abi=abi)

# Settle 1.0 ETH worth of tokens (you can change this)
amount = w3.to_wei(1, "ether")

# Build transaction
nonce = w3.eth.get_transaction_count(sender.address)
gas_price = w3.eth.gas_price

tx = contract.functions.settle(recipient, amount).build_transaction({
    'from': sender.address,
    'nonce': nonce,
    'gas': 250000,
    'gasPrice': gas_price,
})

# Sign and send
signed = w3.eth.account.sign_transaction(tx, private_key)
tx_hash = w3.eth.send_raw_transaction(signed.rawTransaction)

print("✅ Sent transaction:", tx_hash.hex())
