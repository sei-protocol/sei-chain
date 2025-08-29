import qrcode
import hashlib
from datetime import datetime

with open("LumenSigil.txt", "r") as f:
    data = f.read().strip()

sigil_hash = hashlib.sha256(data.encode()).hexdigest()
timestamp = datetime.utcnow().isoformat()
qr_data = f"LumenCard::{sigil_hash}::{timestamp}"

img = qrcode.make(qr_data)
img.save("sigil_qr.png")
print(f"✅ QR code saved as sigil_qr.png for hash: {sigil_hash}")
