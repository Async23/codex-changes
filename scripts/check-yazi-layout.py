#!/usr/bin/env python3
"""Measure the installed Yazi with a copy of its local configuration.

Requires tmux, git, and the configured mobile-auto-layout plugin. Uses an
isolated tmux server and synthetic directories; never changes the source
configuration or interacts with the user's open panes.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import tempfile
import time


OBSERVER = r'''
-- Verification only: observe the final chunks after every original hook.
local original_layout = Tab.layout
function Tab:layout(...)
  original_layout(self, ...)
  local out = io.open(os.getenv("CODEX_LAYOUT_PROBE"), "a")
  if out then
    local tab = self._tab or cx.active
    local h = tab.current.hovered
    local folder = tab.preview.folder
    local count = -1
    if h and folder and tostring(folder.cwd) == tostring(h.url) then
      count = #folder.window
    end
    out:write(string.format("%d\t%d\t%d\t%d\t%s\t%d\n",
      self._area.w, self._chunks[1].w, self._chunks[2].w,
      self._chunks[3].w, h and h.name or "", count))
    out:close()
  end
end
'''


def main():
    config_default = Path(os.environ.get("XDG_CONFIG_HOME", Path.home() / ".config")) / "yazi"
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, default=Path(os.environ.get("YAZI_CONFIG_HOME", config_default)))
    parser.add_argument("--yazi", default=shutil.which("yazi"))
    args = parser.parse_args()
    if not args.yazi or not all(shutil.which(tool) for tool in ("tmux", "git")):
        parser.error("yazi, tmux and git must be available")
    init = (args.config / "init.lua").read_bytes()
    plugin = (args.config / "plugins/mobile-auto-layout.yazi/main.lua").read_bytes()
    result = {
        "version": subprocess.check_output([args.yazi, "--version"], text=True).strip(),
        "init_sha256": hashlib.sha256(init).hexdigest(),
        "layout_plugin_sha256": hashlib.sha256(plugin).hexdigest(),
        "observations": [],
    }
    with tempfile.TemporaryDirectory(prefix="codex-yazi-layout-") as tmp:
        root = Path(tmp)
        config = root / "config"
        # Dereference links so app-side writes can only affect the copied files.
        shutil.copytree(args.config, config)
        (config / "init.lua").write_bytes(init + OBSERVER.encode())
        tree = root / "tree"
        tree.mkdir()
        subprocess.run(["git", "-C", str(tree), "init", "-q"], check=True)
        current = tree / ("p" * 47)
        for name in ("a-empty", "b-long", "c-short", "z" * 90):
            (current / name).mkdir(parents=True)
        (current / "b-long" / ("child-" + "d" * 90)).mkdir()
        (current / "c-short" / "child").mkdir()
        log = root / "widths.tsv"
        base = ["tmux", "-L", f"codex-yazi-layout-{os.getpid()}"]

        def tmux(*arguments):
            return subprocess.check_output(base + list(arguments), text=True)

        def send(*keys):
            tmux("send-keys", "-t", "qa:0.0", *keys)

        def widths(width, hovered, entries=0):
            deadline = time.monotonic() + 5
            candidate, since = None, 0
            while time.monotonic() < deadline:
                rows = log.read_text().splitlines() if log.exists() else []
                if rows:
                    fields = rows[-1].split("\t")
                    if len(fields) == 6 and fields[0] == str(width) and fields[4:] == [hovered, str(entries)]:
                        actual = [int(value) for value in fields[1:4]]
                        if actual != candidate:
                            candidate, since = actual, time.monotonic()
                        elif time.monotonic() - since >= 0.2:
                            return actual
                    else:
                        candidate = None
                time.sleep(0.05)
            raise RuntimeError(f"No layout record for {width} columns, hover {hovered!r}")

        command = shlex.join([
            "env", "-u", "NO_COLOR", "-u", "YAZI_ID",
            f"YAZI_CONFIG_HOME={config}", f"XDG_STATE_HOME={root / 'state'}",
            f"XDG_CACHE_HOME={root / 'cache'}", f"CODEX_LAYOUT_PROBE={log}",
            args.yazi, str(current),
        ])
        try:
            tmux("-f", "/dev/null", "new-session", "-d", "-s", "qa", "-x", "158", "-y", "38", command)
            tmux("set-option", "-g", "status", "off")
            tmux("set-window-option", "-t", "qa:0", "remain-on-exit", "on")
            parent, current_width, _ = widths(158, "a-empty")
            for width in (158, 90, 100, 220):
                tmux("resize-window", "-t", "qa:0", "-x", str(width), "-y", "38")
                send("g", "g")
                expected = [parent, min(current_width, width - parent - 1), max(1, width - parent - current_width)]
                for index, (name, kind) in enumerate((("a-empty", "empty"), ("b-long", "long"), ("c-short", "short"))):
                    if index:
                        send("j")
                    actual = widths(width, name, entries=0 if index == 0 else 1)
                    if actual != expected:
                        raise RuntimeError(f"{width}/{kind}: widths {actual}, expected {expected}")
                    result["observations"].append({"terminal": width, "preview": kind, "widths": actual})
        finally:
            # Wait for Yazi's shutdown/state writes before removing its copy.
            try:
                send("q")
                deadline = time.monotonic() + 5
                while time.monotonic() < deadline:
                    if tmux("display-message", "-p", "-t", "qa:0.0", "#{pane_dead}").strip() == "1":
                        break
                    time.sleep(0.05)
            except subprocess.CalledProcessError:
                pass
            subprocess.run(base + ["kill-server"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
