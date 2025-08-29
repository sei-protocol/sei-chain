import os
import hashlib
from datetime import datetime

wallet = os.urandom(32).hex()
sigil = f"wallet::{wallet}::issued::{datetime.utcnow().isoformat()}"
sigil_hash = hashlib.sha256(sigil.encode()).hexdigest()

with open("~/.lumen_wallet.txt", "w") as w:
    w.write(wallet)

with open("LumenSigil.txt", "w") as s:
    s.write(sigil)

with open("sunset_proof_log.txt", "a") as l:
    l.write(f"{sigil_hash}\n")

print("✅ Sovereign wallet and sigil sealed.")
