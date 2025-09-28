from __future__ import annotations

import argparse

from .app import create_dashboard_app


def main() -> None:
    parser = argparse.ArgumentParser(description="Run the Nova dashboard")
    parser.add_argument("--profile", required=True)
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--port", type=int, default=8050)
    args = parser.parse_args()

    app = create_dashboard_app(args.profile)
    app.run(host=args.host, port=args.port)


if __name__ == "__main__":
    main()
