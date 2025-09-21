# covenant_attestation.py — Minimal REST endpoint for covenant proof
from fastapi import FastAPI
from fastapi.responses import JSONResponse
import uvicorn
import json

app = FastAPI()


@app.get("/covenant/attest")
def attest():
    with open("covenant.json") as f:
        data = json.load(f)
    return JSONResponse({
        "attestation": {
            "source": "SeiGuardian Node Ω",
            "timestamp": int(__import__("time").time()),
            "proof": data
        }
    })


if __name__ == "__main__":
    uvicorn.run(app, port=8742)
