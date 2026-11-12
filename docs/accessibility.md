# Accessibility

GOtome aims at WCAG 2.2 level AA for its web app. This page says where it
stands, what has been checked and by what, what has not, and how to report a
barrier. It is a statement of effort, not a certificate: nobody outside the
project has audited GOtome.

## What is checked on every change

- **axe-core** runs over the sign-in page and every main view, in the light
  and the dark colours, in the desktop browsers and on a phone
  (`e2e/tests/accessibility.spec.ts`). A serious or critical violation fails
  the suite. It needs the package `@axe-core/playwright` in `e2e/`; the
  tests skip until it is installed.
- Each main view has one `h1`, one `main` and one `banner` landmark, and a
  link that skips to the content (the same spec).
- Reflow at 320 CSS pixels and at 200 % zoom, text spacing, visible focus,
  and target sizes on a touch screen (`e2e/tests/layout.spec.ts`, #378, #390).
- Colours: every text and control pair is AA in both themes
  ([design](design.md)).

## Criteria

Met, by design and by the checks above:

- 1.1.1 Non-text content: images that mean something have text; decoration
  is hidden from assistive technology. Charts come with a table.
- 1.3.1 Info and relationships, 1.3.2 Meaningful sequence: headings,
  landmarks, labelled fields, lists and tables are real elements.
- 1.3.4 Orientation, 1.3.5 Identify input purpose.
- 1.4.1 Use of colour, 1.4.3 Contrast (minimum), 1.4.11 Non-text contrast.
- 1.4.4 Resize text, 1.4.10 Reflow, 1.4.12 Text spacing.
- 1.4.13 Content on hover or focus.
- 2.1.1 Keyboard, 2.1.2 No keyboard trap: dialogs hold focus and give it back
  to what opened them; each view has keyboard shortcuts and a "Keys" dialog.
- 2.4.1 Bypass blocks, 2.4.2 Page titled, 2.4.3 Focus order, 2.4.6 Headings
  and labels, 2.4.7 Focus visible, 2.4.11 Focus not obscured (minimum).
- 2.5.1 Pointer gestures: every gesture has a button. 2.5.3 Label in name.
  2.5.8 Target size (minimum): 44 pixels on a touch screen.
- 3.1.1 Language of page: English or German, by the browser's choice.
- 3.3.1 Error identification, 3.3.2 Labels or instructions: a failed request
  is shown in an element with `role="alert"`.
- 4.1.2 Name, role, value; 4.1.3 Status messages: toasts, the notification
  count, upload and bulk-change results, and the readers' page and chapter
  are in live regions.

Following the system's settings:

- `prefers-reduced-motion`: transitions, animations and smooth scrolling are
  cut off everywhere, the finished-book toast appears without moving.
- `prefers-contrast: more`: borders and grey text become the strongest
  colours, and the glass panels become solid.
- `forced-colors` (Windows high contrast): focus rings, buttons, switches,
  and what is pressed, selected or current use system colours.
- `prefers-reduced-transparency` makes the panels opaque.

## Not met, or only in part

- **The books themselves.** An EPUB, MOBI or PDF is the publisher's
  document. GOtome shows it as it is, and a book with no text (a scanned PDF
  without OCR), no headings or no image descriptions stays that way. The
  ebook reader draws the pages in frames made by foliate-js. Browsers expose
  their text, but how a screen reader moves through the pages of a paginated
  frame varies; the reader's own controls (previous and next page, the table
  of contents, the position in per cent and the chapter, announced as they
  change) are ordinary page elements. The PDF reader lays PDF.js's text
  layer over the page, which a screen reader reads, in the order the PDF
  gives; a PDF with a poor reading order reads badly.
- **Read aloud** uses the browser's speech voices, which GOtome does not
  control.
- **Cover images** have the book's title as text, but a cover's own picture
  is not described.
- **1.4.5 Images of text**: covers and the books' own images are what they
  are.
- **Larger text in the system's settings** beyond what 200 % zoom and the
  320 pixel reflow check is not tested.
- **The Tolino app and the KOReader plugin** are separate programs. They have
  not been checked; they follow Android's and KOReader's own conventions.
- **The demo** is a static build of the same app, with the same limits.

## Not tested

No session with a screen reader (NVDA, JAWS, VoiceOver, TalkBack) has been
held yet. What is said above about announcements comes from reading the code
and the page's accessibility tree, and from the automated checks, not from
listening. Voice control, switch access and magnifiers have not been tried
either. If you use one, your report is the most useful thing you can give.

## Reporting a barrier

Open an issue at
[github.com/praetorianer777/GoTome/issues](https://github.com/praetorianer777/GoTome/issues)
with the label `accessibility`, and say what you were doing, what you
expected, what happened, and which browser and assistive technology you use.
You do not need to reproduce it in detail: "the Keys dialog is not announced
in VoiceOver" is enough. If you would rather not write in public, use the
private route described in [SECURITY.md](../SECURITY.md) and say that it is
an accessibility report, not a vulnerability. Barriers that stop someone
from using a main view are treated like bugs that stop everyone.

## Legal note

Germany's Barrierefreiheitsstärkungsgesetz (BFSG) and the European
Accessibility Act apply to certain products and services offered to
consumers. GOtome is a self-hosted tool that each person runs for
themselves, and the project does not offer a service; it is not assumed to be
bound by them. The project states where it stands because an open project
should, and holds itself to WCAG 2.2 AA as the yardstick.
