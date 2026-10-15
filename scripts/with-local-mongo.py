#!/usr/bin/env python3
"""Run a command with local MongoDB credentials without printing/copying secrets.

Only reads the existing Docker MongoDB .env. Does not change its files or users.
Example: python3 scripts/with-local-mongo.py go run ./cmd/server
"""

import os
import shlex
import sys
from pathlib import Path
from urllib.parse import quote


def main():
    if len(sys.argv) < 2:
        sys.exit("用法：python3 scripts/with-local-mongo.py <命令> [参数…]")
    env = os.environ.copy()
    if not env.get("MONGO_URI"):
        path = Path(
            env.get("LOCAL_MONGO_ENV", str(Path.home() / "docker/mongodb/.env"))
        )
        config = {}
        for line in path.read_text().splitlines():
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            key, value = line.removeprefix("export ").split("=", 1)
            # Handle common dotenv quoting without evaluating any shell code.
            tokens = shlex.split(value, comments=True)
            config[key.strip()] = " ".join(tokens)
        user = quote(config["MONGO_ROOT_USER"], safe="")
        password = quote(config["MONGO_ROOT_PASSWORD"], safe="")
        port = int(config.get("MONGO_PORT", "27017"))
        env["MONGO_URI"] = (
            f"mongodb://{user}:{password}@127.0.0.1:{port}/?authSource=admin&directConnection=true"
        )
    env.setdefault("MONGO_DB", "questionnaire_lab1")
    env.setdefault("INTEGRATION_MONGO_URI", env["MONGO_URI"])
    os.execvpe(sys.argv[1], sys.argv[1:], env)


if __name__ == "__main__":
    main()
