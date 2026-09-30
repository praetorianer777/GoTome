#!/usr/bin/env bash
# Generates the web app's icons and artwork from the originals in
# assets/branding. The results are checked in, so this only runs when an
# original changes. Needs ImageMagick 7 (magick) with WebP support.
set -euo pipefail

web="$(cd "$(dirname "$0")/.." && pwd)"
src="$web/../assets/branding"
public="$web/public"
assets="$web/src/assets"
mkdir -p "$public" "$assets"

icon() { magick "$src/icon.png" -resize "$1x$1" -strip "$2"; }

# The tab icon, in the three sizes browsers pick from.
magick "$src/icon.png" -strip -define icon:auto-resize=48,32,16 "$public/favicon.ico"
icon 180 "$public/apple-touch-icon.png"
icon 192 "$public/icon-192.png"
icon 512 "$public/icon-512.png"

# The mark in the app's header, shown at 28 px and sharp at three times that.
magick "$src/icon.png" -resize 84x84 -strip -quality 90 "$assets/mark.webp"

# The illustration on the setup and sign-in pages, shown at up to 320 px wide
# and sharp at twice that.
magick "$src/artwork.jpg" -resize 640x -strip -quality 82 "$assets/artwork.webp"

identify "$public"/*.png "$public/favicon.ico" "$assets"/*.webp | awk '{print $1, $3}'
