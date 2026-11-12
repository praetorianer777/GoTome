# Organising a library's files

GOtome can rename the files of a library and sort them into folders by their
books' details, so that the folder on disk reads as an archive: an author's
folder, a series' folder, a number and a title. It is under **Libraries**, in
a library's **Organise files** section, and it is off the moment you do not
use it: nothing moves until you press the button.

## Which libraries

- A **managed** library may always be organised.
- An **existing folder** (external library) only when it is writable
  ("GOtome may change files here") **and** you have switched on "Let GOtome
  rename and sort the files of this library". GOtome never touches the
  layout of an external library otherwise.
- A read-only library cannot be organised.

Files stay inside their library (and on its file system). Nothing is
deleted, and files in the trash, missing files, placeholders and merged
books are left alone.

## The pattern

Each library has a pattern, the same default for all:

```
{author}/[{series}/][{series_index} - ]{title}.{ext}
```

A book in a series lies at `Jane Austen/Austen Novels/02 - Emma.epub`, one
without at `Jane Austen/Emma.epub`.

| Placeholder | Is |
| --- | --- |
| `{author}` | the first author, or "Unknown author" |
| `{author_sort}` | the same as "Austen, Jane" |
| `{title}` | the title, or "Untitled" |
| `{series}` | the series; empty without one |
| `{series_index}` | the place in the series, at least two digits (`02`, `01.5`); empty without a series |
| `{year}` | the year of publication; empty when unknown |
| `{language}` | the language code; empty when unknown |
| `{ext}` | the file's extension, in lower case |

- `/` separates folders. The pattern must end in `{ext}` and use `{title}`
  outside square brackets, so that two books cannot share a name.
- Text in `[square brackets]` is left out when a placeholder in it is empty.
  Brackets do not nest. A folder whose name comes out empty is left out.
- Values are made safe: Unicode in its composed form (NFC); `/ \ : * ? " < > |`
  and control characters become `_`; runs of blanks become one space; no
  name starts with a dot (a dot name is hidden, and scans pass dot folders
  by) or ends with a dot or a space; names Windows reserves (`CON`, `NUL`,
  `COM1` and the like) get a leading `_`.
- Lengths: a folder name is cut at 120 bytes, a file name at 160, the whole
  path at 240, always at a character, with the extension kept. A path that
  cannot fit even with a short name is left where it is and listed.
- Two files that get the same path (compared without regard to case) are
  told apart by a number: the first in the order author, series, place,
  title, then ID keeps the plain name, the next ones become `Emma (2).epub`,
  `Emma (3).epub`. A path that something untracked already holds is taken,
  too. The numbers are the same every time you look.
- A file that already lies where the pattern puts it is not touched, so a
  second run changes nothing. A path another file holds now is never moved
  onto, even by a file that moves away in the same run, so a swap of two
  names takes two runs.
- **Audiobooks** with several parts lie in one folder named like the book's
  file would be (`Homer/Odyssey/`), the parts numbered in their order:
  `01 - Odyssey.mp3`, `02 - Odyssey.mp3`.

## Preview and apply

**Preview** asks what the pattern, as typed, would do, and moves nothing: how
many files would move, how many are in place already, how many get a number,
which stay and why (a missing file, a path too long), and the first hundred
moves as from and to. **Save pattern** keeps it for the library; **Use the
default pattern** resets it.

**Move N files** starts a run, which works in the background, a book at a
time. It moves with each file:

- the KOReader folder `name.sdr` and a cover or package file of the same
  name (`name.jpg`, `.jpeg`, `.png`, `.webp`, `.gif`, `.opf`), unless
  another book file shares that name, or something lies at their new place;
- the folders it made, and it removes the folder a file left only if that is
  empty now and nothing else is in it.

A book is moved in one transaction: the records, then the files. If a rename
fails (a folder that cannot be written, another file system), the book's
files are put back and the book is listed as "Not moved" with the reason;
the rest go on, and a run stops after twenty failures. A scan that is
running delays the run until it is done.

Reading places, devices' queues, KOReader's progress and everything else
that belongs to a book is kept by the file's ID, so nothing is lost; the
next scan finds the same files in their new places.

## Undoing

The run is listed under **Changes** as "Files organised". Undoing it (a
book, or the whole run) moves the files and their sidecars back, as long as
they are where the run left them and the old places are free; otherwise the
book is reported as changed since and left as it is.

## Measuring

Nothing in the gate reaches your library. `make test-integration` runs the
tests on a temporary one: preview, apply, collisions, a refused rename that
is taken back, a scan afterwards and undo.
