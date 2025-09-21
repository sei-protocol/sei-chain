from __future__ import annotations

import argparse

import uvicorn

from .server import create_app


def main() -> None:
    parser = argparse.ArgumentParser(description="Run the Nova API server")
    parser.add_argument("--profile", required=True)
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--port", type=int, default=8000)
    parser.add_argument("--token", default=None)
    args = parser.parse_args()

    app = create_app(args.profile, token=args.token)
    uvicorn.run(app, host=args.host, port=args.port)


if __name__ == "__main__":
    main()
