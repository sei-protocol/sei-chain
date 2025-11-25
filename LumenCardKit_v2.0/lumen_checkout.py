import hashlib, time

with open("LumenSigil.txt", "r") as f:
    sigil = f.read().strip()

checkout_hash = hashlib.sha256((sigil + str(time.time())).encode()).hexdigest()
print(f"🔐 Ephemeral Checkout Session ID: {checkout_hash}")
