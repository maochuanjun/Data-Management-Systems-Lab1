#!/usr/bin/env python3
"""Run a command with this project's .env; explicit environment wins.

The config path is relative to this script, independent of the current directory.
No external Docker configuration is read and no secret values are printed.
"""

import os
import re
import shlex
import sys
from pathlib import Path


def command_environment(path, inherited):
    env = dict(inherited)
    if path.is_file():
        for number, line in enumerate(path.read_text().splitlines(), start=1):
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            key, separator, value = line.removeprefix("export ").partition("=")
            key = key.strip()
            if not separator or not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", key):
                raise ValueError(f"项目.env第{number}行不是有效的变量赋值")
            try:
                # Parse dotenv quoting without executing shell expressions.
                tokens = shlex.split(value, comments=True)
            except ValueError:
                raise ValueError(f"项目.env第{number}行引号不完整") from None
            env.setdefault(key, " ".join(tokens))
    if not env.get("MONGO_URI"):
        raise ValueError("请在项目.env或环境变量中配置MONGO_URI；可参考.env.example")
    env.setdefault("MONGO_DB", "questionnaire_lab1")
    env.setdefault("INTEGRATION_MONGO_URI", env["MONGO_URI"])
    return env


def main():
    if len(sys.argv) < 2:
        sys.exit("用法：python3 scripts/with-local-mongo.py <命令> [参数…]")
    path = Path(__file__).resolve().parent.parent / ".env"
    try:
        env = command_environment(path, os.environ)
    except (ValueError, OSError) as error:
        sys.exit(str(error))
    os.execvpe(sys.argv[1], sys.argv[1:], env)


if __name__ == "__main__":
    main()
