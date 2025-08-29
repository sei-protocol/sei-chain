import smtplib
from email.message import EmailMessage

receiver = "your@email.com"  # 🔧 Replace manually

msg = EmailMessage()
msg["Subject"] = "Your LumenCard Wallet + Sigil"
msg["From"] = "noreply@lumen.local"
msg["To"] = receiver

msg.set_content("Attached is your sovereign wallet and sigil.")
msg.add_attachment(open("sigil_qr.png", "rb").read(), maintype="image", subtype="png", filename="sigil_qr.png")
msg.add_attachment(open("~/.lumen_wallet.txt", "rb").read(), maintype="text", subtype="plain", filename="wallet.txt")

with smtplib.SMTP("localhost") as s:
    s.send_message(msg)

print("✅ Email sent locally (verify SMTP setup).")
