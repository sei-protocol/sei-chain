import requests, yaml

with open("config.yaml") as f:
    config = yaml.safe_load(f)

def send_alert(msg):
    if not config["telegram"]["enabled"]:
        return
    token = config["telegram"]["token"]
    chat_id = config["telegram"]["chat_id"]
    url = f"https://api.telegram.org/bot{token}/sendMessage"
    requests.post(url, data={"chat_id": chat_id, "text": msg})
