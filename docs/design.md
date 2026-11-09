# The web app's design

What the web app looks like and why, so that every page is made the same way.
Decided for #239, redirected by #359; the owner may revisit any of it (the open questions are at
the end). The tokens live in `web/src/styles/index.css`, the shared pieces in
`web/src/components/`.

## Principles

The direction changed with #359: the owner's draft (`docs/design/draft-2026-11.jpg`)
goes further than the first design. Its text labels are placeholders from an
image generator; the layout and the look are what count.

- **A midnight library.** The look comes from GOtome's own artwork
  (`assets/branding/`): the gopher's cyan on a near-black, blue night, and
  candlelight on leather and parchment. Dark is the signature theme; light
  is the same library by day, on parchment, with the same layout.
- **Panels over a backdrop.** The page is a dim, blurred library at night
  (dark theme; parchment in the light one). Everything a person reads or
  uses sits on a **panel**: a rounded, slightly glassy surface of the
  neutral scale (`bg-slate-900/90` with a soft border and a blur behind it;
  `bg-white/90` in the light theme). The sidebar, the top bar and the main
  content are panels. Text is never set straight on the backdrop.
- **The books carry the page.** Covers are the largest and most colourful
  thing on a page. Everything around them is quiet: muted text, colour only
  where it means something (the accent for what can be done or is chosen,
  amber for what is being read or rated, red for danger).
- **A fixed frame, pages that look alike.** Top bar, sidebar, content panel;
  nothing has to be looked for twice.
- **Mascots and art are allowed, in the margins.** The gopher may stand in a
  corner of the sidebar or a page, a hero may open the home page, a round
  badge on a cover says the medium. They are decoration (`alt=""`), never
  carry information alone, never sit behind text, and add no animation
  beyond a short fade. A soft cyan glow marks focus and the current
  navigation entry; a faint warm one may mark what is being read.
- **Nothing for a few.** Keyboard and screen-reader use (#76) keeps working
  everywhere: landmarks, one visible focus ring, dialogs that hold focus,
  the skip link, every text through `t()`.

### What stays, what changes (#359)

| Stays | Changes |
|---|---|
| Every colour token and the neutral scale; contrast of every text and ground pair (AA, 4.5:1 and 3:1) | "No images behind text" becomes: images behind the page, never behind text (panels carry the text) |
| The one focus ring and its glow | "Subtle, not kitsch" becomes: art in the margins, a hero on the home page, badges on covers |
| Serif headings, system fonts, no web fonts | The sidebar is a panel grouped by medium; the top bar carries the sections |
| `NAV_ITEMS` with permissions; the drawer below 1024 px | Corners: `rounded-lg` controls, `rounded-xl` cards, `rounded-2xl` panels, `rounded-full` the search field and avatars |
| Keyboard shortcuts, landmarks, skip link | The library list is no longer the start page (the home page is, #359 step 3) |

A backdrop or panel may never lower a text's contrast under the ratios
below: panels are at least 90 % opaque over a backdrop dimmed to a
luminance far below the page's, which `e2e/tests/accessibility.spec.ts`
(axe, once installed) checks on the main views.

## Colour

Sampled from the artwork, the icon and the owner's draft
(`docs/design/draft-2026-11.jpg`, #384):

| Name | Value | From |
|---|---|---|
| ink | `#0b1630` | the dark theme's page: a deep midnight blue, not grey |
| brand (cyan) | `#45bde0` | the "GO" and the gopher; accent on dark: the focus ring, primary buttons, badges, the avatar |
| brand-strong | `#0b6f96` | the cyan darkened to read on parchment and white (5.2:1) |
| candle (amber) | `#e69a46` | candle flames; "reading now", ratings on dark; the warm vignette at the page's edges |
| candle-strong | `#8a4f0f` | the same for light (6.1:1 on parchment) |
| cream (gold) | `#e8c88a` | the wordmark's "tome"; page titles on dark, sparingly elsewhere (9.6:1 on a panel) |
| gold-strong | `#7a5a14` | the gold darkened for text on parchment and white (5.9:1 and 6.4:1) |
| leather | `#52271b` | book spines; used sparingly, never behind text |

The neutral scale is Tailwind's `slate`, redefined in `@theme` to run from
parchment (50) to midnight navy (950), so every page that already uses
`slate` takes the theme without a change:

| Step | Value | Light theme | Dark theme |
|---|---|---|---|
| 50 | `#faf6ec` | page (parchment) | |
| 100 | `#f1e9d8` | hovered rows, sidebar | text |
| 200 | `#e3d8c2` | lines | |
| 300 | `#c8bca6` | control borders | strong text |
| 400 | `#9fb0c8` | | muted text: authors, meta (7.0:1 on a panel, 5.2:1 on 800) |
| 500 | `#5b6a80` | text said in passing (≥ 4.5:1 on 50) | (shown as 400) |
| 600 | `#536480` | muted text (5.6:1 on 50, 5.0:1 on 100) | control borders (2.6:1 on a panel; was 2.3) |
| 700 | `#2c3f5f` | | panel borders, lines |
| 800 | `#2a3a58` | | the current tab and navigation entry (a lighter blue-grey pill with white text), hovered rows |
| 900 | `#16243f` | text | panels, cards, menus (translucent navy, 90 %) |
| 950 | `#0f1b36` | | the drawer |

The dark page is the ink blue with a faint warm amber vignette at its
corners and the backdrop image under both; panels are translucent navy with
a 1 pixel border of step 700 and no hard shadow, the bars blur what is
behind them. Section headings in the sidebar are uppercase white; authors
and meta are step 400. Every pair of text and ground used is at least WCAG
AA (4.5:1, 3:1 for large text and controls); a new pair is checked before
it is used.

The **backdrop** (dark theme) is an image, blurred and dimmed
(`web/src/assets/backdrop.webp`, made by `web/scripts/branding.sh`) until it
is far darker than any panel; in the light theme the page is parchment. It
is seen only between and around panels.


## Type

- **Headings and the wordmark**: a serif, the "tome" of the artwork. Only
  what the system has (`--font-serif`: Iowan Old Style, Palatino, Book
  Antiqua, Georgia): no web fonts, as the Content Security Policy allows no
  other origin and the app carries no font files.
- **Everything else**: the system sans (`--font-sans`, Tailwind's default).
- **The scale**: page title `text-2xl` serif semibold; section `text-lg`
  serif medium; body `text-base`; controls, tables and secondary text
  `text-sm`; footnotes and badges `text-xs`. No other sizes, except these
  deliberate ones: the book title (`text-3xl`, `sm:text-4xl`, one per page),
  the 404 heading (`text-3xl`), the wordmark and the stars of a rating
  (`text-xl`), and the figures of the overview (`text-xl`). The review
  heading's `text-xl` is not one of them: it becomes `text-lg`.
- **Line length**: running text (descriptions, introductions, help, notes)
  is 45 to 75 characters a line: `max-w-prose` (65ch). `max-w-2xl` (about 85
  characters) is for forms and tables, never for a paragraph. Cards and
  narrow columns may be shorter than 45.
- **Line height**: body text 1.4 to 1.6: the size's own (`text-sm` 1.43,
  `text-base` 1.5) or `leading-normal`. `leading-relaxed` (1.625) is over
  the range and not used; headings may be tighter (`leading-tight`).

## Spacing and shape

- Tailwind's 4-pixel steps (`1`, `2`, `3`, `4`, `6`, `8`) for gaps, margins
  and padding. The half steps `0.5`, `1.5` and `2.5` (2, 6 and 10 pixels)
  are allowed **inside a control or a row of one** (padding, an icon's gap,
  a pill), never for the space between sections, cards or blocks, and
  `3.5` is not used.
- A page's sections are `gap-8` apart, a section's parts `gap-3`, controls
  in a row `gap-2`, cards in a grid or a row `gap-4`; the frame's panels
  are `gap-4` apart.
- Corners: `rounded-lg` controls and menu items, `rounded-xl` cards and
  popovers, `rounded-2xl` panels and dialogs, `rounded-full` the search
  field, the avatar, tabs' backgrounds and badges.
- Content is at most `max-w-6xl` wide inside its panel; forms and text at
  most `max-w-2xl`, paragraphs at most `max-w-prose`.
- A touch target is at least 24 by 24 pixels, and 44 by 44 on a phone.

## Layout and navigation

- **Desktop (≥ 1024 px)**: a top bar and, under it, the sidebar panel and the
  content panel side by side, over the backdrop.
  - The **top bar** (a panel across the width, sticky) holds the wordmark
    (icon, "GO" in cyan sans, "tome" in cream serif) at the left, the
    **section tabs** (a `nav` named "Sections": Library, Wishlist, New
    books, Statistics; the current one in a rounded pill, `aria-current`),
    then the rounded quick search (with the gopher's mark inside its right
    edge), the notification bell with its count and the avatar menu.
  - The **sidebar panel** (a `nav` named "Main", scrolling by itself) is
    grouped by medium, each group with a heading and an outline icon:
    **Ebooks** (all ebooks, authors, series, collections, search) and
    **Audiobooks** (all audiobooks), then a "More" group (overview,
    profile) and the groups for work on the catalogue (Add books, Review,
    Duplicates, Clean up, Genres, Trash, Jobs) and administration
    (Libraries, Users, Settings). The two last fold; each remembers per
    browser whether it is folded, and opens by itself on one of its pages.
    Every entry has an outline icon; the current one is highlighted
    (accent colour, soft glow) and carries `aria-current="page"`. The gopher
    stands at the panel's foot, decoration only.
  - The **content panel** holds the page, `max-w-6xl` centred.
- **Phone and tablet (< 1024 px)**: no sidebar and no tabs. The top bar has
  a menu button, the wordmark, the bell and the avatar; the quick search
  takes a second row. The menu button opens one drawer from the left (a
  modal dialog: focus inside, Escape and the backdrop close it, a choice
  closes it) holding the sections first, then the sidebar's groups. The
  content panel runs edge to edge without its side borders.
- **The account menu** (avatar and the name where there is room) holds who is
  signed in, Profile, E-readers, Language, Colours and Sign out (#217).
- The **readers and the player** keep the frame out of the way: the content
  panel has no padding or border and there is no footer.
- Which entries anyone sees is still `NAV_ITEMS` with their permissions;
  an entry without one is for everyone signed in. An entry is in the sidebar
  by its `group`; those of the group `sections` are the tabs on a desktop
  and the drawer's first group.

## Pages and components

The parts are in `web/src/components/ui.tsx` (`button`, `control`, `panel`,
`card`, `PageHeader`, `EmptyState`, `Skeleton`, `Loading`) and
`shell.tsx`; a page takes its classes from there.

- **Page header**: the title (h1), with the count where the page lists
  something, a sentence of introduction where needed (`max-w-prose`), and the
  page's primary action at the right (wrapping under the title on a phone).
- **Buttons**: primary (filled accent), secondary (outline), danger (red
  outline, filled red only in the confirmation). Links that act like buttons
  look like buttons. **One primary button per view**: the single thing the
  page is for (a dialog is a view of its own).
- **Panels** (`panel`): the frame's surfaces: `rounded-2xl`, a soft border,
  90 % opaque, blurred behind. **Cards** (`card`): raised surface inside a
  panel, `rounded-xl`, one thin line, no shadow except on menus and dialogs.
- **Cover cards** (#359 step 3): a card with the cover, title and author
  below it, each truncated to one line, and a round badge on the cover's
  corner naming the medium (a book for an ebook, headphones for audio; the
  badge has an accessible name and the card's text says it too).
- **Empty states**: a dashed box with a heading, a sentence and, where the
  viewer can, the action that fills it.
- **Loading**: skeletons of the shape to come (grey blocks the size of
  covers or rows) where a list loads; a plain "Loading…" only for small
  parts.
- **Covers** keep their own ratio (2:3 frame, the image contained), a
  shadow below like a book on a shelf, and a warm ring for a book being
  read.
- **Dialogs** (`components/dialog.tsx`) and popovers (notifications, the
  account menu) are panels with a shadow.
- **Home page** (#359 step 2): a hero banner, then Recent additions and
  Recommended for you as rows of cover cards (a scrolling row on a phone).
  The hero is art: it carries the page's h1 as text beside it, not inside
  the image.

## Rules for every page

- **Three clicks**: the key tasks, opening a book, reading it and sending it
  to a device, take at most three clicks from the library (or the home
  page). A new page that adds a step to one of them says why.
- **Phone book page**: the cover, the title, the authors and the primary
  action ("Read" or "Listen") are in the first screen of a phone (320 by
  568 pixels) without scrolling; the rest follows.
- **One primary button per view**, as above.
- **Reading width**: running text is `max-w-prose`.
- **Contrast**: every pair is AA, in both themes, over any backdrop.
- Layout checks that enforce these are #378's; this document is what they
  measure against.

## Phone, tablet, desktop

All three matter. Phone: one column, the drawer, the quick search across
the top bar's second row, filters in a sheet, rows of cards that scroll
sideways. Tablet: the phone's frame with the desktop's grids. Desktop: the
tabs and the sidebar. Nothing on any page may be
wider than the screen (`smoke.spec.ts` checks it on the phone).

## Open for the owner

- The artwork the draft shows and GOtome does not have yet (hero, corner
  mascots, backdrop, medium badges): until it exists the pages use the
  current art (`assets/branding/`), generated through
  `web/scripts/branding.sh`. #359's follow-up issue lists what is wanted.
- The palette and the serif are taken from the artwork without mock-ups;
  a hue or the serif stack is changed in `index.css` alone.
- The order and grouping of the navigation, and whether Profile belongs in
  it besides the account menu.
- Whether the dark theme should be the default for "As the device" on
  light devices (it is not: the device decides).
