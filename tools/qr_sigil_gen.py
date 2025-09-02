# qr_sigil_gen.py — Generates QR sigil for SeiNet covenant
import json
import qrcode
import sys


def generate_sigil(covenant_json, outfile="sigil.png"):
    data = json.dumps(covenant_json, separators=(",", ":"))
    img = qrcode.make(data)
    img.save(outfile)
    print(f"✅ QR sigil written to {outfile}")


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python3 qr_sigil_gen.py covenant.json")
        sys.exit(1)

    with open(sys.argv[1]) as f:
        covenant = json.load(f)

    generate_sigil(covenant)
