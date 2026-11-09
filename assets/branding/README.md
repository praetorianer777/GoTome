# Branding

The product name is written **GOtome**. Identifiers keep their own spelling: the
binary `gotome`, the Go module path, `GOTOME_*` variables, the repository URL.

| File | Size | Use |
|---|---|---|
| `banner.jpg` | 1024 × 1024 | Top of the repository README |
| `artwork.jpg` | 1024 × 1054 | Earlier illustration for the login and setup pages; the hero replaced it there |
| `logo.jpg` | 1024 × 1051 | Logo with wordmark |
| `icon.png` | 800 × 800 | The logo's mark alone, square; source for the favicon and app icons |
| `hero.jpg`, `hero-light.jpg` | 1584 × 672 | Home page hero, night and day (gopher at the left, calm space at the right) |
| `backdrop.jpg`, `backdrop-light.jpg` | 1408 × 768 | The library at night and by day, behind the panels of the dark and light theme |
| `mascot-sidebar.jpg` | 1408 × 768 | Four views of the gopher holding a book; the front one stands at the sidebar's foot |
| `mascot-corner.jpg` | 1408 × 768 | The waving gopher in the corner of the content panel's foot (desktop) |
| `gopher-sleep.jpg` | 1024 × 1024 | Empty states: nothing here yet |
| `gopher-search.jpg` | 1024 × 1024 | No results of a search or a filter |
| `gopher-confused.jpg` | 1024 × 1024 | Page not found, errors, a missing author or series |
| `gopher-reading.jpg` | 1024 × 1024 | Loading a whole page |
| `gopher-cheer.jpg` | 1024 × 1024 | The toast when a book is marked finished |
| `badge-ebook.jpg`, `badge-audio.jpg` | 1024 × 1024 | The medium's round badge on a cover |
| `badge-both.jpg` | 1408 × 768 | The same for a book with an ebook and an audiobook |
| `wordmark-go.jpg`, `wordmark-go-light.jpg` | 1408 × 768 | The illuminated "GO" of the wordmark, cyan for the dark theme and teal for the light one |
| `fonts/UnifrakturCook-Bold.ttf` | | The font "tome" of the wordmark is set in; a build source only, the app loads no font |
| `gopher-sheet.jpg` | 1408 × 768 | Character reference only; not shipped |

The web app's icons, header mark and sign-in illustration are generated from
these files by `web/scripts/branding.sh` and checked in under `web/public` and
`web/src/assets`. Run the script again after replacing an original.

The five `gopher-*` stickers, the mascots and the badges have a white ground
that the script cuts away (a fill from the border, the stickers' white edge
included, then a softened alpha) and writes as transparent WebP at the size
the app shows them, sharp at twice that: about 300 KB in all, the backdrops
and heroes included. The JPEG files are the originals. `icon.png` is cut from `logo.jpg` and
padded to a square with the logo's own background colour, `rgb(2, 12, 22)`.

The gopher is based on the Go gopher, designed by Renée French and licensed under
[CC BY 4.0](https://creativecommons.org/licenses/by/4.0/).

The wordmark is built by the same script: the "GO" is cut out of its
original and "tome" is set in UnifrakturCook Bold, filled with a gold
gradient and outlined in brown, at two thirds of the GO's height. The font is
by j. 'mach' wust (Peter Wiegel) and licensed under the
[SIL Open Font License 1.1](fonts/OFL.txt).
