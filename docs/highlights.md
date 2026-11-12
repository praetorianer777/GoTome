# Highlights and notes

A highlight is a passage you marked in a book, with a colour and, if you
like, a note. They are yours alone, like your reading status: nobody else
sees them, not even an administrator, and they show only for books in
libraries you still see.

## In the readers

In the EPUB, MOBI and AZW reader and in the PDF reader, select text. A bar
appears at the bottom: choose a colour, write a note if you want one, and
**Highlight**. The reader draws the passage in its colour and lists it
under **Highlights** (key `h`); in the ebook reader a tap on a highlight
opens that list, and **Show in the book** goes to it.

- Highlights in a PDF are drawn over the page, which works where the PDF
  has text to select; a scanned PDF without a text layer cannot be
  highlighted.
- A highlight is kept at the place in the file it was made in. The reader
  of another file of the same book (an EPUB and a Kindle file, say) lists
  it and finds the passage by its words, but does not draw it.
- Highlights from KOReader are drawn by KOReader, and are listed in the web
  reader: their place is KOReader's own and the browser cannot go to it
  exactly, so "Show in the book" looks for the passage's words.

## The Highlights page

**Highlights** in the menu lists every highlight and note you have,
newest first. Search by words of the passage, the note, the title or an
author. Each can have its note changed, its colour changed, or be deleted.
A book's page lists its own the same way.

## Exporting

**Export as Markdown** and **Export as CSV** download what the page lists,
all of it or what the search matches; on a book's page, that book's. The
Markdown has a heading per book, each passage as a quotation with your note
under it. The CSV has a row per highlight: title, authors, chapter, page,
passage, note, colour and when it was made. A cell that begins like a
formula is written with a quote before it, so that a spreadsheet shows it.

## From KOReader

The GOtome plugin sends them; see [koreader.md](koreader.md). They arrive
as highlights of the book the file belongs to, marked as coming from that
device, and follow what the device does: a changed note is changed here, a
deleted highlight is deleted here. Two KOReader devices that both hold the
book each keep their own copy of a highlight.

## Kobo readers

A Kobo reader keeps its highlights on the device and sends them to Kobo's
own store account. GOtome's Kobo sync (the line the profile shows for a
reader) carries books, reading places and shelves, but has no endpoint for
annotations, and a reader synced with GOtome does not send them, so they
are not brought across. A Kobo reader that runs KOReader with the GOtome
plugin sends them like any other.

## Backups and the API

Highlights are in a [backup](backup-and-upgrade.md) like the rest of
your data. The API has them under `/api/v1/annotations`
(`personal:manage`); a device's token reaches only the send of a book's
highlights, `PUT /api/v1/device/annotations/{document}`.
