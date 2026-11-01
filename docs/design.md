# The web app's design

What the web app looks like and why, so that every page is made the same way.
Decided for #239; the owner may revisit any of it (the open questions are at
the end). The tokens live in `web/src/styles/index.css`, the shared pieces in
`web/src/components/`.

## Principles

- **A midnight library.** The look comes from GOtome's own artwork
  (`assets/branding/`): the gopher's cyan on a near-black, blue night, and
  candlelight on leather and parchment. Dark is the signature theme; light
  is the same library by day, on parchment.
- **The books carry the page.** Covers are the largest and most colourful
  thing on a page. Everything around them is quiet: few borders, muted
  text, colour only where it means something (the accent for what can be
  done or is chosen, amber for what is being read or rated, red for danger).
- **Calm and content-first**, in the spirit of Audiobookshelf, Kavita and
  Komga: a fixed frame (sidebar, top bar) and pages that look alike, so
  nothing has to be looked for twice.
- **Subtle, not kitsch.** No textures, no images behind text, no
  animations beyond a short fade. A soft cyan glow marks focus and the
  current navigation entry; a faint warm one may mark what is being read.
- **Nothing for a few.** Keyboard and screen-reader use (#76) keeps working
  everywhere: landmarks, one visible focus ring, dialogs that hold focus,
  the skip link, every text through `t()`.

## Colour

Sampled from the artwork and the icon:

| Name | Value | From |
|---|---|---|
| ink | `#020c16` | the logo's ground; the dark theme's page |
| brand (cyan) | `#37b5db` | the gopher; accent on dark |
| brand-strong | `#0b6f96` | the cyan darkened to read on parchment and white (5.2:1) |
| candle (amber) | `#e69a46` | candle flames; "reading now", ratings on dark |
| candle-strong | `#8a4f0f` | the same for light (6.1:1 on parchment) |
| cream | `#e5d2ab` | the wordmark's "tome", parchment |
| leather | `#52271b` | book spines; used sparingly, never behind text |

The neutral scale is Tailwind's `slate`, redefined in `@theme` to run from
parchment (50) to ink navy (950), slightly blue throughout, so every page
that already uses `slate` takes the theme without a change:

| Step | Value | Light theme | Dark theme |
|---|---|---|---|
| 50 | `#faf6ec` | page (parchment) | |
| 100 | `#f1e9d8` | hovered rows, sidebar | text |
| 200 | `#e3d8c2` | lines | |
| 300 | `#c8bca6` | control borders | strong text |
| 400 | `#94a3b8` | | muted text (≥ 5.4:1 on 800) |
| 500 | `#5b6a80` | text said in passing (≥ 4.5:1 on 50) | (shown as 400) |
| 600 | `#4a5870` | muted text (6.7:1 on 50) | control borders |
| 700 | `#33425a` | | lines |
| 800 | `#1b2d47` | | hovered rows, lines |
| 900 | `#0e2038` | text | cards, menus |
| 950 | `#071526` | | sidebar, top bar |

The page itself is ink in the dark. Every pair of text and ground used is
at least WCAG AA (4.5:1, 3:1 for large text and controls); a new pair is
checked before it is used.

## Type

- **Headings and the wordmark**: a serif, the "tome" of the artwork. Only
  what the system has (`--font-serif`: Iowan Old Style, Palatino, Book
  Antiqua, Georgia): no web fonts, as the Content Security Policy allows no
  other origin and the app carries no font files.
- **Everything else**: the system sans (`--font-sans`, Tailwind's default).
- Scale: page title `text-2xl` serif semibold; section `text-lg` serif
  medium; body `text-base`; controls, tables and secondary text `text-sm`;
  footnotes and badges `text-xs`. No other sizes.

## Spacing and shape

- Tailwind's 4-pixel steps only. A page's sections are `gap-8` apart, a
  section's parts `gap-3`, controls in a row `gap-2`.
- Corners `rounded-md` for controls, `rounded-lg` for cards and menus.
- Content is at most `max-w-6xl` wide; forms and text at most `max-w-2xl`.

## Layout and navigation

- **Desktop (≥ 1024 px)**: a sidebar on the left, always there, with the
  wordmark (icon, "GO" in cyan sans, "tome" in cream serif) and the
  navigation in groups: the library (Library, Authors, Series, Search, Collections,
  Wishlist, New books, Your reading, Overview, Profile), work on the
  catalogue (Add books, Review, Duplicates, Clean up, Trash, Jobs) and
  administration (Libraries, Users, Settings). The two lower groups fold;
  each remembers per browser whether it is folded, and opens by itself on
  one of its pages. A top bar over the page holds the quick search, the
  notifications and the account menu.
- **Phone and tablet (< 1024 px)**: no sidebar. The top bar has a menu
  button that opens the same navigation as a drawer from the left (a modal
  dialog: focus inside, Escape and the backdrop close it, a choice closes
  it).
- **The account menu** at the top right (initials, and the name where
  there is room) holds who is signed in, Profile, E-readers, Language,
  Colours and Sign out (#217).
- The **readers and the player** keep the frame out of the way: no footer.
- Which entries anyone sees is still `NAV_ITEMS` with their permissions;
  an entry without one is for everyone signed in.

## Pages and components

- **Page header**: the title (h1), with the count where the page lists
  something, a sentence of introduction where needed, and the page's
  primary action at the right (wrapping under the title on a phone).
- **Buttons**: primary (filled accent, one per view), secondary (outline),
  danger (red outline, filled red only in the confirmation). Links that
  act like buttons look like buttons.
- **Cards**: raised surface (`white` / `slate-900`), `rounded-lg`, one thin
  line, no shadow except on menus and dialogs.
- **Empty states**: a dashed box with a heading, a sentence and, where the
  viewer can, the action that fills it.
- **Loading**: skeletons of the shape to come (grey blocks the size of
  covers or rows) where a list loads; a plain "Loading…" only for small
  parts.
- **Covers** keep their own ratio (2:3 frame, the image contained), a
  shadow below like a book on a shelf, and a warm ring for a book being
  read.
- **Dialogs** (`components/dialog.tsx`) and popovers (notifications, the
  account menu) share the card's surface with a shadow.

## Phone, tablet, desktop

All three matter. Phone: one column, the drawer, the quick search across
the top bar's second row, filters in a sheet. Tablet: the phone's frame
with the desktop's grids. Desktop: the sidebar. Nothing on any page may be
wider than the screen (`smoke.spec.ts` checks it on the phone).

## Open for the owner

- The palette and the serif are taken from the artwork without mock-ups;
  a hue or the serif stack is changed in `index.css` alone.
- The order and grouping of the navigation, and whether Profile belongs in
  it besides the account menu.
- Whether the dark theme should be the default for "As the device" on
  light devices (it is not: the device decides).
