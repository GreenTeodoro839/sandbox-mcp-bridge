"""Assemble the KernelSU/Magisk flashable module zip: text files at the zip root
with LF endings, the binary marked executable (0755).

Usage:
    python3 build_zip.py [binary_path] [out_zip]
Defaults: ./local-bridge-android  ->  ./local-mcp-bridge.zip
The android-arm64 binary must already be built (see README / CI workflow).
"""

import os
import sys
import zipfile

ROOT = os.path.dirname(os.path.abspath(__file__))
MOD = os.path.join(ROOT, "ksu-module")
BIN = sys.argv[1] if len(sys.argv) > 1 else os.path.join(ROOT, "local-bridge-android")
OUT = sys.argv[2] if len(sys.argv) > 2 else os.path.join(ROOT, "local-mcp-bridge.zip")

TEXT = [
    "module.prop",
    "service.sh",
    "customize.sh",
    "META-INF/com/google/android/update-binary",
    "META-INF/com/google/android/updater-script",
]
BINARY_ARC = "local-bridge-android"


def add(z, arc, data, mode):
    zi = zipfile.ZipInfo(arc)
    zi.external_attr = mode << 16
    zi.compress_type = zipfile.ZIP_DEFLATED
    z.writestr(zi, data)


with zipfile.ZipFile(OUT, "w", zipfile.ZIP_DEFLATED) as z:
    for arc in TEXT:
        with open(os.path.join(MOD, arc), "rb") as f:
            data = f.read().replace(b"\r\n", b"\n").replace(b"\r", b"\n")
        add(z, arc, data, 0o644)
    with open(BIN, "rb") as f:
        add(z, BINARY_ARC, f.read(), 0o755)

print("wrote", OUT)
with zipfile.ZipFile(OUT) as z:
    for i in z.infolist():
        print(f"  {oct(i.external_attr >> 16)}  {i.file_size:>9}  {i.filename}")
