#!/bin/sh
# Makes the files of this folder. GOtome's own FFmpeg neither decodes nor
# encodes, so this runs under a full FFmpeg (Debian's, in bookworm's 5.1).
set -eu
cd "$(dirname "$0")"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
ffmpeg -v error -f lavfi -i color=c=0x909090:s=60x90 -frames:v 1 "$work/cover.png"

# make <file> <seconds> <cover or nothing>, with the FFMETADATA on stdin.
make() {
  file=$1 seconds=$2 cover=${3:-}
  cat >"$work/meta.txt"
  set -- -v error -y -f lavfi -i "sine=frequency=440:duration=$seconds" -i "$work/meta.txt"
  [ -z "$cover" ] || set -- "$@" -i "$work/cover.png"
  set -- "$@" -map 0:a -map_metadata 1 -map_chapters 1 -ac 1
  [ -z "$cover" ] || set -- "$@" -map 2:v -c:v png -disposition:v attached_pic
  case $file in
  *.m4b) set -- "$@" -c:a aac -b:a 32k -f ipod ;;
  *.mp3) set -- "$@" -c:a libmp3lame -b:a 32k -ar 22050 -id3v2_version 3 ;;
  *.ogg) set -- "$@" -c:a libvorbis -ar 22050 ;;
  *.opus) set -- "$@" -c:a libopus -b:a 24k ;;
  esac
  ffmpeg "$@" "$file"
}

make dune.m4b 3 cover <<'META'
;FFMETADATA1
title=Dune
artist=Frank Herbert
composer=Scott Brick; Orlagh Cassidy
genre=Science Fiction
date=2007-01-01
comment=The desert planet.
[CHAPTER]
TIMEBASE=1/1000
START=0
END=500
title=Opening Credits
[CHAPTER]
TIMEBASE=1/1000
START=500
END=2000
title=Book One
[CHAPTER]
TIMEBASE=1/1000
START=2000
END=3000
title=Book Two
META

for ext in mp3 flac ogg opus m4b; do
  case $ext in ogg | opus) cover= ;; *) cover=cover ;; esac
  make "emma.$ext" 1.5 $cover <<'META'
;FFMETADATA1
title=Chapter Three
album=Emma
artist=Jane Austen
track=3/10
disc=2/2
META
done

make chapter-seven.mp3 1 <<'META'
;FFMETADATA1
title=Chapter Seven
track=7
META

# The parts of a book, whose tags say another order than their numbers.
number=1
for track in 2 3 1; do
  make "part-$number.mp3" 1 <<META
;FFMETADATA1
album=Emma
artist=Jane Austen
title=Part $track
track=$track
META
  number=$((number + 1))
done

make three-chapters.m4b 3 <<'META'
;FFMETADATA1
title=Dune
artist=Frank Herbert
[CHAPTER]
TIMEBASE=1/1000
START=0
END=1000
title=Book One
[CHAPTER]
TIMEBASE=1/1000
START=1000
END=2000
title=Book Two
[CHAPTER]
TIMEBASE=1/1000
START=2000
END=3000
title=Book Three
META
