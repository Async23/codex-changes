#!/usr/bin/env python3
"""Render an ANSI terminal capture of --demo as a self-contained SVG.

Usage: tmux capture-pane -peN -t <demo-pane> | python3 scripts/render-preview.py
Only synthetic demo captures are accepted. No browser or image assets required.
"""

import argparse
import html
import re
import sys
import unicodedata


SGR = re.compile(r"\x1b\[([0-9;:]*)m")
BASIC = [
    "#171a1d", "#f0a3a3", "#9ad9ab", "#e4ce93",
    "#94bada", "#c7a8d9", "#84cfc2", "#e5e9eb",
    "#647178", "#f0a3a3", "#9ad9ab", "#e4ce93",
    "#94bada", "#c7a8d9", "#84cfc2", "#ffffff",
]


def color256(index):
    if index < 16:
        return BASIC[index]
    if index >= 232:
        level = 8 + 10 * (index - 232)
        return f"#{level:02x}{level:02x}{level:02x}"
    index -= 16
    cube = [0, 95, 135, 175, 215, 255]
    return "#" + "".join(f"{cube[n]:02x}" for n in [index // 36, (index // 6) % 6, index % 6])


def columns(text):
    return sum(0 if unicodedata.combining(char) else 2 if unicodedata.east_asian_width(char) in "WF" else 1 for char in text)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--theme", choices=["dark", "light"], default="dark")
    args = parser.parse_args()
    default_fg, default_bg = ("#333333", "#faf4ed") if args.theme == "light" else ("#e5e9eb", "#171a1d")
    data = sys.stdin.read()
    plain = SGR.sub("", data)
    if "DEMO · 合成数据" not in plain:
        raise SystemExit("Refusing to publish a capture without the synthetic demo marker")
    if re.search(r"/Users/|/home/|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-", plain):
        raise SystemExit("Refusing a capture containing personal paths or real-looking session IDs")
    lines = data.rstrip("\n").splitlines()
    width = max(columns(SGR.sub("", line)) for line in lines)
    cell, leading, inset, top = 9.6, 23, 20, 51
    canvas_w, canvas_h = width * cell + inset * 2, len(lines) * leading + top + 14
    output = [
        f'<svg xmlns="http://www.w3.org/2000/svg" width="{canvas_w:g}" height="{canvas_h:g}" viewBox="0 0 {canvas_w:g} {canvas_h:g}">',
        '<title>Codex Changes — synthetic terminal demo</title>',
        '<desc>A file list on the left and the selected file’s chronological patches on the right. All data is synthetic.</desc>',
        f'<rect width="{canvas_w:g}" height="{canvas_h:g}" rx="10" fill="{default_bg}"/>',
        '<g fill="#647178"><circle cx="22" cy="20" r="4"/><circle cx="38" cy="20" r="4"/><circle cx="54" cy="20" r="4"/></g>',
        '<text x="78" y="25" fill="#9ba5ac" font-family="monospace" font-size="13">Codex Changes — DEMO</text>',
        '<g font-family="Menlo, DejaVu Sans Mono, Noto Sans Mono CJK SC, monospace" font-size="16">',
    ]
    for row, line in enumerate(lines):
        fg, bg, bold, dim, inverse = default_fg, default_bg, False, False, False
        column = 0
        for part in re.split(r"(\x1b\[[0-9;:]*m)", line):
            match = SGR.fullmatch(part)
            if match:
                codes = [int(value) if value else 0 for value in match[1].replace(":", ";").split(";")]
                i = 0
                while i < len(codes):
                    code = codes[i]
                    if code == 0:
                        fg, bg, bold, dim, inverse = default_fg, default_bg, False, False, False
                    elif code == 1:
                        bold = True
                    elif code == 2:
                        dim = True
                    elif code == 7:
                        inverse = True
                    elif code == 22:
                        bold = dim = False
                    elif code == 27:
                        inverse = False
                    elif code in (38, 48) and i + 2 < len(codes):
                        if codes[i + 1] == 5:
                            value = color256(codes[i + 2])
                            i += 2
                        elif codes[i + 1] == 2 and i + 4 < len(codes):
                            value = "#" + "".join(f"{v:02x}" for v in codes[i + 2:i + 5])
                            i += 4
                        else:
                            i += 1
                            continue
                        if code == 38:
                            fg = value
                        else:
                            bg = value
                    elif 30 <= code <= 37:
                        fg = BASIC[code - 30]
                    elif 40 <= code <= 47:
                        bg = BASIC[code - 40]
                    elif 90 <= code <= 97:
                        fg = BASIC[code - 90 + 8]
                    elif 100 <= code <= 107:
                        bg = BASIC[code - 100 + 8]
                    elif code == 39:
                        fg = default_fg
                    elif code == 49:
                        bg = default_bg
                    i += 1
                continue
            part = re.sub(r"[\x00-\x1f\x7f]", "", part)
            if not part:
                continue
            span_width = columns(part) * cell
            x, y = inset + column * cell, top + row * leading
            ink, paper = (bg, fg) if inverse else (fg, bg)
            if paper != default_bg:
                output.append(f'<rect x="{x:g}" y="{y - 17:g}" width="{span_width:g}" height="{leading:g}" fill="{paper}"/>')
            if part.strip():
                output.append(f'<text x="{x:g}" y="{y:g}" fill="{ink}" font-weight="{700 if bold else 400}" opacity="{0.76 if dim else 1}" xml:space="preserve" textLength="{span_width:g}" lengthAdjust="spacingAndGlyphs">{html.escape(part)}</text>')
            column += columns(part)
    output.extend(["</g>", "</svg>"])
    sys.stdout.write("\n".join(output) + "\n")


if __name__ == "__main__":
    main()
