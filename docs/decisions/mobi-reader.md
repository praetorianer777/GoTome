# Reading MOBI, AZW and AZW3

Decided in #22, 2026-09-30.

## Question

MOBI and the Kindle formats built on it need metadata, cover and text. No
maintained Go reader was known. The issue asked for a reader of our own and for
an evaluation of `mobitool` (libmobi) as a subprocess fallback.

## Decision

GOtome reads these files with its own pure-Go package, `internal/format/mobi`.
`mobitool` is not installed in the app image and is not a fallback. It is
installed in the test toolchain image, where it checks the reader's fixtures.

## Reasons

- **The format is small.** It is a PalmDB container, record 0 with the
  PalmDOC, MOBI and EXTH headers, text records compressed with PalmDOC LZ77 or
  HUFF/CDIC, and, for KF8, an FDST record that separates the book from its
  style sheets. The reader is under 700 lines including decompression.
- **It is memory-safe and fuzzed.** Every offset is checked before it is
  followed, decompression stops at a limit, and dictionary phrases that
  refer to themselves are caught. Two fuzz targets run in the gate: whole files
  and the decompressors alone.
- **libmobi is C that parses untrusted input.** It has had several
  memory-safety fixes found by fuzzing. Running it would have meant a
  subprocess under `procexec` for every file, for no gain: it cannot read
  protected files either.
- **libmobi is still useful as a second opinion.** The tests build their
  fixtures with a writer in the test code. A writer and a reader from one
  author could share a misreading of the format, so `TestMobitoolReadsTheFixtures`
  has libmobi 0.11 (Debian bookworm's `libmobi-tools`) read the same files. Its
  metadata and its decompressed markup must match. That check found a HUFF
  record in the fixtures that was shorter than real ones.

## Limits

- Protected files are catalogued with their metadata and cover, which Kindle
  files keep in the clear, and flagged. Their text is not read.
- The fixtures are built by the tests, not taken from Kindle tools or real
  books. The benchmark corpus holds EPUBs only; real Kindle files, such as the
  ones Project Gutenberg also offers, are the check still to be made. A file
  that is read wrongly goes into `testdata` with the fix.
- Only the first flow of a KF8 file is read as text. Tables of contents and
  chapter boundaries are not extracted, so the text is one section.
