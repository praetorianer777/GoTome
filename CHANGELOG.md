# Changelog

What changes for someone who runs or uses GOtome. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- GOtome runs as two containers, the app and its database, started with one
  `docker compose` file. Images are published for amd64 and arm64.
- First-run setup creates the administrator account in the browser.
- Sign in with user name and password. Sessions last 30 days without use, and
  repeated wrong passwords are slowed down.
- Three roles: Administrator, Editor and Reader.
- A web app with light and dark colours that also works on a phone. The library
  view is still empty: importing books comes next.
- Libraries. An administrator adds a library that GOtome manages itself, or
  points GOtome at an existing folder, which it only reads unless allowed to
  change files. A library is shared with everybody or private to its owner and
  members. Removing a library from GOtome never deletes its folder or files.
- Scanning. GOtome looks through a library's folder when the library is added,
  every six hours (`GOTOME_SCAN_INTERVAL`, `0` to switch it off), and when an
  Editor or Administrator presses "Scan now". It takes in EPUB, PDF, MOBI, AZW3
  and audio files, keeps the formats of one title and the parts of an audiobook
  together as one book, follows files that were moved or renamed, and marks
  files that are gone as missing. It never deletes, moves or changes a file.
  The books it finds cannot be browsed yet.
- Book details are read from EPUB and PDF files: title, authors, series,
  publisher, language, description, tags, ISBN, page count and cover. A PDF
  without a text layer (a scan) is imported and marked as such. The app image
  now includes poppler to read PDFs.
