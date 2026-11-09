#!/usr/bin/env bash
# Generates the web app's icons and artwork from the originals in
# assets/branding. The results are checked in, so this only runs when an
# original changes. Needs ImageMagick 7 (magick) with WebP support.
set -euo pipefail

web="$(cd "$(dirname "$0")/.." && pwd)"
src="$web/../assets/branding"
public="$web/public"
assets="$web/src/assets"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
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

# The backdrops, the library at night for the dark theme and by day for the
# light one, blurred a little. The night one is dimmed to a fifth less and the
# day one washed with parchment, so that, with the panels 90 % opaque on top,
# the text on them keeps its contrast (docs/design.md).
magick "$src/backdrop.jpg" -resize 1280x -blur 0x3 -evaluate multiply 0.8 -strip -quality 55 "$assets/backdrop.webp"
magick "$src/backdrop-light.jpg" -resize 1280x -blur 0x3 -fill '#faf6ec' -colorize 20% -strip -quality 55 "$assets/backdrop-light.webp"

# The hero of the home page, at most 1152 px wide and sharp at 1.2 times that.
magick "$src/hero.jpg" -resize 1400x -strip -quality 72 "$assets/hero.webp"
magick "$src/hero-light.jpg" -resize 1400x -strip -quality 72 "$assets/hero-light.webp"

# The originals of everything below have a white ground. The ground is cut
# away by filling from the border with a fuzz, which stops at the characters'
# dark outline; the sticker edge the five gophers have is white as well and
# goes with it. The alpha is then eroded by a pixel and softened, so that no
# light fringe of the old ground is left around the outline on a dark page.
cutout() {
	magick "$1" -alpha set -bordercolor white -border 2 -fuzz "${3:-12}%" -fill none \
		-draw 'color 0,0 floodfill' -shave 2x2 \
		\( +clone -alpha extract -morphology erode disk:1.5 -blur 0x0.8 \) \
		-compose copy_opacity -composite -trim +repage "${@:4}" -strip \
		-define webp:alpha-quality=90 -quality 82 "$2"
}

# The gopher at the foot of the sidebar: the front view of the sheet, shown
# at 144 px tall and sharp at twice that.
magick "$src/mascot-sidebar.jpg" -crop 372x720+22+50 +repage "$tmp/side.png"
cutout "$tmp/side.png" "$assets/mascot-sidebar.webp" 12 -resize x288
# The gopher waving in a page's corner, shown at 64 px wide.
cutout "$src/mascot-corner.jpg" "$assets/mascot-corner.webp" 12 -resize 128x
# The gophers of the empty, search, error, loading and finished states, shown
# at up to 160 px.
for g in sleep search confused reading cheer; do
	cutout "$src/gopher-$g.jpg" "$assets/gopher-$g.webp" 22 -resize 320x
done
# The medium's badge on a cover, shown at 24 px and sharp at 2.7 times that.
for b in ebook audio both; do
	cutout "$src/badge-$b.jpg" "$assets/badge-$b.webp" 12 -resize 64x64\!
done

# The wordmark: the illuminated GO of the owner's art, with "tome" set in
# UnifrakturCook Bold (assets/branding/fonts, SIL OFL) and filled with a
# gradient of darker gold at the top and bottom and a bright band across the
# middle, outlined in dark brown, at 66 % of the GO's height and bottom
# aligned. Night (cyan GO, bright gold) and day (teal GO, darker gold). The
# font is only a source: the app loads none and shows these images.
font="$src/fonts/UnifrakturCook-Bold.ttf"

# A word of the font filled with a three-stop vertical gradient and outlined.
gild() {
	local top=$1 sheen=$2 bottom=$3 line=$4 dst=$5 w h a
	magick -background none -font "$font" -pointsize 400 -fill white -stroke white -strokewidth 10 \
		label:tome -trim +repage "$tmp/mask.png"
	w=$(identify -format %w "$tmp/mask.png")
	h=$(identify -format %h "$tmp/mask.png")
	a=$((h / 2))
	magick \( -size "${w}x${a}" "gradient:${top}-${sheen}" \) \
		\( -size "${w}x$((h - a))" "gradient:${sheen}-${bottom}" \) -append "$tmp/grad.png"
	magick -background none -font "$font" -pointsize 400 -fill "$line" -stroke "$line" -strokewidth 10 \
		label:tome -trim +repage "$tmp/line.png"
	magick -background none -font "$font" -pointsize 400 -fill white label:tome -trim +repage \
		-resize "${w}x${h}!" "$tmp/inner.png"
	magick "$tmp/grad.png" \( "$tmp/inner.png" -alpha extract \) -alpha off -compose CopyOpacity -composite "$tmp/fill.png"
	magick "$tmp/line.png" "$tmp/fill.png" -gravity center -compose over -composite "$dst"
}

# The GO with its white ground, the counters of the letters included, cut
# away; the edge is eroded a pixel and softened, as for the gophers.
wordmark() {
	local go=$1 top=$2 sheen=$3 bottom=$4 line=$5 name=$6 h
	magick "$src/$go" -alpha set -fuzz 14% -transparent white \
		\( +clone -alpha extract -morphology erode disk:1.5 -blur 0x0.8 \) \
		-compose copy_opacity -composite -trim +repage "$tmp/go.png"
	gild "$top" "$sheen" "$bottom" "$line" "$tmp/tome.png"
	h=$(identify -format %h "$tmp/go.png")
	magick "$tmp/tome.png" -resize "x$((h * 66 / 100))" "$tmp/tome-fit.png"
	magick -background none "$tmp/go.png" -size "$((h / 25))x1" xc:none "$tmp/tome-fit.png" \
		-gravity south +append "$tmp/word.png"
	# The top bar shows it 36 px high and sharp at three times that; the
	# sign-in and setup pages about 280 px wide and sharp at twice that.
	magick "$tmp/word.png" -resize x108 -strip -define webp:alpha-quality=90 -quality 85 "$assets/$name.webp"
	magick "$tmp/word.png" -resize 560x -strip -define webp:alpha-quality=90 -quality 85 "$assets/$name-large.webp"
}
wordmark wordmark-go.jpg '#b07c26' '#fcebb0' '#8a5a18' '#4a3014' wordmark
wordmark wordmark-go-light.jpg '#8a6020' '#d6ad55' '#6a4612' '#3a2410' wordmark-light

identify "$public"/*.png "$public/favicon.ico" "$assets"/*.webp | awk '{print $1, $3}'
