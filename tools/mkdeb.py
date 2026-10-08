#!/usr/bin/env python3
"""Build a .deb without dpkg: python3 tools/mkdeb.py VERSION ARCH BINARY OUT.deb

ARCH is a Debian arch: amd64, arm64 or armhf. The package installs:
  /usr/bin/townsquare
  /usr/lib/systemd/user/townsquare.service   (systemctl --user enable --now townsquare)
  /usr/share/applications/townsquare.desktop and an icon (desktop Linux, Raspberry Pi OS)
  /usr/share/doc/townsquare/copyright
Updates after install come from Townsquare's own signed updater (stored in ~/.townsquare/bin).
"""
import gzip, hashlib, io, os, sys, tarfile, time

version, arch, binary, out = sys.argv[1:5]
root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
v = version.lstrip("v")
mtime = int(time.time())

UNIT = """[Unit]
Description=Townsquare: one calendar to schedule all your community posts
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/bin/townsquare serve
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
"""

DESKTOP = """[Desktop Entry]
Type=Application
Name=Townsquare
Comment=One calendar to schedule all your community posts
Exec=townsquare
Icon=townsquare
Terminal=true
Categories=Office;Network;
"""

COPYRIGHT = """Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/
Upstream-Name: Townsquare
Source: https://github.com/nerveband/townsquare

Files: *
License: AGPL-3.0-only
 See https://www.gnu.org/licenses/agpl-3.0.txt
"""

POSTINST = """#!/bin/sh
set -e
if [ "$1" = configure ]; then
  echo "Townsquare is installed. Start it with:  townsquare"
  echo "Or run it in the background at login:   systemctl --user enable --now townsquare"
  echo "(On a headless Raspberry Pi also run: sudo loginctl enable-linger \\$USER)"
fi
"""


def add(tar, name, data, mode=0o644):
    ti = tarfile.TarInfo(name)
    ti.size, ti.mode, ti.mtime, ti.uid, ti.gid, ti.uname, ti.gname = len(data), mode, mtime, 0, 0, "root", "root"
    tar.addfile(ti, io.BytesIO(data))


def add_dir(tar, name):
    ti = tarfile.TarInfo(name)
    ti.type, ti.mode, ti.mtime, ti.uname, ti.gname = tarfile.DIRTYPE, 0o755, mtime, "root", "root"
    tar.addfile(ti)


files = [
    ("./usr/bin/townsquare", open(binary, "rb").read(), 0o755),
    ("./usr/lib/systemd/user/townsquare.service", UNIT.encode(), 0o644),
    ("./usr/share/applications/townsquare.desktop", DESKTOP.encode(), 0o644),
    ("./usr/share/icons/hicolor/512x512/apps/townsquare.png", open(os.path.join(root, "web/ui/public/icon-512.png"), "rb").read(), 0o644),
    ("./usr/share/doc/townsquare/copyright", COPYRIGHT.encode(), 0o644),
]
dirs = sorted({"/".join(f[0].split("/")[:i]) for f in files for i in range(2, f[0].count("/") + 1)})

data_buf = io.BytesIO()
with tarfile.open(fileobj=data_buf, mode="w:gz", format=tarfile.GNU_FORMAT) as tar:
    for d in dirs:
        add_dir(tar, d)
    for name, data, mode in files:
        add(tar, name, data, mode)

md5sums = "".join(f"{hashlib.md5(d).hexdigest()}  {n[2:]}\n" for n, d, _ in files)
size_kb = sum(len(d) for _, d, _ in files) // 1024
control = f"""Package: townsquare
Version: {v}
Architecture: {arch}
Maintainer: Townsquare <https://github.com/nerveband/townsquare>
Installed-Size: {size_kb}
Depends: ca-certificates
Recommends: ffmpeg
Section: net
Priority: optional
Homepage: https://github.com/nerveband/townsquare
Description: One calendar to schedule all your community posts
 Schedule posts to WhatsApp groups, communities, channels and Status, and to
 Telegram groups, channels, topics and stories, from one calendar. Runs on
 your own computer. Updates itself with signed releases.
"""
ctrl_buf = io.BytesIO()
with tarfile.open(fileobj=ctrl_buf, mode="w:gz", format=tarfile.GNU_FORMAT) as tar:
    add(tar, "./control", control.encode())
    add(tar, "./md5sums", md5sums.encode())
    add(tar, "./postinst", POSTINST.encode(), 0o755)


def ar_member(name, data):
    hdr = f"{name:<16}{mtime:<12}{0:<6}{0:<6}{'100644':<8}{len(data):<10}`\n".encode()
    return hdr + data + (b"\n" if len(data) % 2 else b"")


with open(out, "wb") as f:
    f.write(b"!<arch>\n")
    f.write(ar_member("debian-binary", b"2.0\n"))
    f.write(ar_member("control.tar.gz", ctrl_buf.getvalue()))
    f.write(ar_member("data.tar.gz", data_buf.getvalue()))
print("wrote", out)
