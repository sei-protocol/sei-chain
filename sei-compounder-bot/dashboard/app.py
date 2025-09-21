from __future__ import annotations
from datetime import datetime
from pathlib import Path
from typing import List

from flask import Flask, jsonify, render_template

BASE_DIR = Path(__file__).resolve().parent.parent
LOG_FILE = BASE_DIR / "bot" / "logs" / "compound.log"
CONFIG_FILE = BASE_DIR / "bot" / "config.yaml"

app = Flask(__name__)


def _tail(path: Path, lines: int = 50) -> List[str]:
    if not path.exists():
        return []
    with path.open("r", encoding="utf-8") as handle:
        content = handle.readlines()
    return content[-lines:]


@app.route("/")
def status():
    logs = _tail(LOG_FILE)
    return render_template("status.html", logs=logs)


@app.route("/healthz")
def healthz():
    return {"status": "ok", "timestamp": datetime.utcnow().isoformat()}


@app.route("/config")
def config_view():
    if not CONFIG_FILE.exists():
        return jsonify({"error": "config missing"}), 404
    with CONFIG_FILE.open("r", encoding="utf-8") as handle:
        return app.response_class(handle.read(), mimetype="application/x-yaml")


@app.route("/logs")
def logs_view():
    return jsonify({"lines": _tail(LOG_FILE, lines=200)})


if __name__ == "__main__":
    app.run(debug=True, port=8080)
