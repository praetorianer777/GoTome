# Fixture audio

Two six-second tones, the parts of an audiobook for the player's browser
tests: `part1.mp3` marks two chapters, `part2.mp3` none. They were made with
the ffmpeg of the test toolchain image:

    ffmpeg -f lavfi -i "sine=frequency=440:duration=6" -i meta.txt \
      -map_metadata 1 -map_chapters 1 -ac 1 -ar 22050 -b:a 32k -id3v2_version 3 part1.mp3
    ffmpeg -f lavfi -i "sine=frequency=660:duration=6" -metadata title="The Lighthouse" \
      -ac 1 -ar 22050 -b:a 32k -id3v2_version 3 part2.mp3

where `meta.txt` is an FFMETADATA1 file with the title and the chapters
Arrival (0–3 s) and The Keeper (3–6 s).
