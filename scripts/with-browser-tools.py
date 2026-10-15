#!/usr/bin/env python3
"""Discover existing browser test tools; never install application dependencies."""
import os
from pathlib import Path
import subprocess
import sys


def node_output(source, *args):
    result = subprocess.run(
        ["node", "-e", source, *args], capture_output=True, text=True, check=False
    )
    return result.stdout.strip() if result.returncode == 0 else ""


def main():
    if len(sys.argv) < 2:
        sys.exit("用法：python3 scripts/with-browser-tools.py <命令> [参数…]")
    env = os.environ.copy()
    module = env.get("PLAYWRIGHT_MODULE")
    if not module:
        module = node_output("process.stdout.write(require.resolve('playwright'))")
    if not module:
        candidates = list((Path.home() / ".npm/_npx").glob("*/node_modules/playwright"))
        candidates += [Path.cwd() / "node_modules/playwright"]
        global_root = subprocess.run(
            ["npm", "root", "-g"], capture_output=True, text=True, check=False
        )
        if global_root.returncode == 0:
            candidates.append(Path(global_root.stdout.strip()) / "playwright")
        installed = [p for p in candidates if p.exists()]
        if installed:
            module = str(max(installed, key=lambda p: p.stat().st_mtime))
    if not module:
        sys.exit("E2E 需要已有的 Playwright 安装；请设置 PLAYWRIGHT_MODULE 指向该模块。")
    env["PLAYWRIGHT_MODULE"] = module
    if not env.get("PLAYWRIGHT_EXECUTABLE_PATH"):
        default = node_output(
            "process.stdout.write(require(process.argv[1]).chromium.executablePath())", module
        )
        if default and Path(default).is_file():
            env["PLAYWRIGHT_EXECUTABLE_PATH"] = default
        else:
            cache = Path.home() / ".cache/ms-playwright"
            candidates = list(cache.glob("chromium_headless_shell-*/chrome-headless-shell-linux64/chrome-headless-shell"))
            candidates += list(cache.glob("chromium-*/chrome-linux64/chrome"))
            candidates += [Path("/usr/bin/chromium"), Path("/opt/google/chrome/chrome")]
            installed = [p for p in candidates if p.is_file()]
            if not installed:
                sys.exit("E2E 需要 Chromium；请设置 PLAYWRIGHT_EXECUTABLE_PATH 指向已有浏览器。")
            env["PLAYWRIGHT_EXECUTABLE_PATH"] = str(max(installed, key=lambda p: p.stat().st_mtime))
    os.execvpe(sys.argv[1], sys.argv[1:], env)


if __name__ == "__main__":
    main()
