#!/usr/bin/env bash
# Download the static ffmpeg and ffprobe that go inside the Mac app, check them
# against the SHA-256 sums pinned below, and print the folder they are in:
#   <dir>/arm64/{ffmpeg,ffprobe,versions.txt}  <dir>/amd64/{ffmpeg,ffprobe,versions.txt}
# Builds are by Martin Riedl (https://ffmpeg.martin-riedl.de), FFmpeg 9.0.2, GPL v3.
# Cached in ~/.cache/townsquare/ffmpeg so releases don't need the network. To move to
# a new FFmpeg: change VER, the two build ids and the four sums (shasum -a 256 the zips),
# update third_party/ffmpeg/NOTICE.txt, and check the new build with `otool -L`.
set -euo pipefail
VER=9.0.2
BASE=https://ffmpeg.martin-riedl.de/download/macos
ID_ARM64=1789931890_$VER
ID_AMD64=1789931006_$VER
SUM_arm64_ffmpeg=c8ed4c4e6978a03c485edbfe4e0a5dc2380f8a30bba5150531b31b094492d924
SUM_arm64_ffprobe=fcbe839537485eaee7a7a8bc5cbc0f90d53617e80943e8a5b2e31cb851197ea6
SUM_amd64_ffmpeg=7c6b4125b191cbf773832dc51f424cf2b6bb7da43007d1e066f95909e47cacd4
SUM_amd64_ffprobe=2322438ed2f6319a691291b247d09c69dcaa3a982460d1f269a7e1af335cfdfd

OUT="${TOWNSQUARE_FFMPEG_CACHE:-$HOME/.cache/townsquare/ffmpeg}/$VER"
for arch in arm64 amd64; do
  if [ "$arch" = arm64 ]; then id="$ID_ARM64"; else id="$ID_AMD64"; fi
  d="$OUT/$arch"; mkdir -p "$d"
  for t in ffmpeg ffprobe; do
    want="SUM_${arch}_$t"; want="${!want}"
    if [ -x "$d/$t" ] && [ "$(cat "$d/$t.zip.sum" 2>/dev/null)" = "$want" ]; then continue; fi
    curl -fsSL --retry 3 -o "$d/$t.zip" "$BASE/$arch/$id/$t.zip"
    got="$(shasum -a 256 "$d/$t.zip" | cut -d' ' -f1)"
    [ "$got" = "$want" ] || { echo "ffmpeg: $arch $t.zip checksum mismatch (got $got)" >&2; rm -f "$d/$t.zip"; exit 1; }
    unzip -oq "$d/$t.zip" "$t" -d "$d"
    chmod 755 "$d/$t"; echo "$want" > "$d/$t.zip.sum"
  done
  [ -s "$d/versions.txt" ] || curl -fsSL --retry 3 -o "$d/versions.txt" "$BASE/$arch/$id/versions.txt"
done
echo "$OUT"
