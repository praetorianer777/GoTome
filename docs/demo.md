# The demo

A clickable copy of GOtome on GitHub Pages (#396): the web app runs against a
fake API in the browser, with public-domain books, so nothing is installed and
no server is involved. It is built by `.github/workflows/demo.yml` on each
release tag and by hand; the owner enables Pages once (Settings, Pages,
Source: GitHub Actions).

## How it works

- `make web-demo` builds the web app in Vite's mode `demo` into `web/dist-demo`
  with the base path `/GoTome/` (`DEMO_BASE` changes it). Only that build holds
  the demo: `web/src/demo` is left out of every other one.
- `web/src/main.tsx` starts `demo/boot.ts` first. It builds a `FakeServer`
  (`web/src/fake/server.ts`, the one the app's tests run on) from
  `web/src/demo/library.json`, answers every `/api/v1/*` request of `fetch`
  with it, and tells the typed client where covers and files are (`assetUrl`,
  `api/client.ts`), as an image or an audio element does not go through fetch.
  The router takes the base path.
- The person is signed in as `demo`, an administrator, and the sign-in and
  sign-out are not there. Anything done to a book, a collection or a reading
  state lives in the tab and is gone on reload. The banner (`DemoBanner`) says so.
- Pages that write to a library or change the server (upload, the admin pages,
  jobs, trash, review, clean-up, genres, editing and looking up a book) show
  "Not in the demo" (`DemoNote`, `notInTheDemo` in `demo/mode.ts`).
- Pages answers an address it has no file for with `404.html`; the build copies
  `index.html` there, so a deep link or a reload opens the app at that address.
- Nothing in the demo talks to another origin. The CSP and the security
  headers are the server's and do not apply on Pages.

## The sample data

`web/src/demo/library.json` is the whole catalogue: books (title, authors,
series and place, tags, language, description, reading state, `source`),
similar books, collections, smart shelves, a duplicate pair, what is followed
and the new books of it, and a passage for the text search. A book's files are
`files` (the repository's own fixtures, copied from their `source` path at
build) or, for a book with a `source.url`, `web/demo/files/<id>.epub`, which
`make sample-books` fetches. What exists at build time is copied to
`demo/files` and listed in `demo/files.json`; a book without a file is shown
without one. A book without a cover image gets one drawn from its title
(`demo/cover.ts`); `web/demo/covers/<id>.jpg` replaces it.

Until the books are fetched the demo holds 33 books and 2 wishes, all with metadata,, two EPUBs
from `e2e/fixtures/books` and an audiobook of two tones from
`e2e/fixtures/audio`; its size is about 4 MB. With the books fetched it is the
EPUBs and their covers, a few hundred kilobytes each (the cover is taken as it
lies in the EPUB; resizing them to small WebP is a follow-up).

`make sample-books` (the owner, or CI, as it reaches the internet) takes each
`source.url` and checks the file against `web/demo/sample-books.lock.json`.
Where there is no entry yet it records the SHA-256 and size, so the first run
pins the hashes: commit the file, and every later fetch must match. A file
already in `web/demo/files` is kept if it matches. Each download is tried three
times with a growing pause, and a failure says its cause (`ETIMEDOUT`, a TLS
error, the HTTP status), not only "fetch failed". A book that cannot be had is
reported and the rest go on. `make sample-books SAMPLE_BOOKS_FROM=<dir>`
(a directory in the checkout) reads `<dir>/<id>.epub` first, where it matches
the lock file, and downloads only what is missing. The download addresses
(`source.url`, the pages below with the EPUB's name added) were written without
being fetched, so the first run may find some wrong; the Kafka and Gutenberg
ones are the least certain.

## Where CI gets the books

Standard Ebooks at times refuses GitHub's runners while it serves other
machines, so `demo.yml` has three sources, each checked against the lock file:

1. The cache (on `web/demo/files`, keyed by the hash of the lock file). A
   run restores only caches of its own ref or of `main`, and `demo.yml` runs on
   tags, so `demo-books.yml` saves it from `main` (on a push that changes the
   lock file or the fetch scripts, and by hand), only once every book matches
   the lock; `demo.yml` only restores it. A changed lock file starts from
   nothing until that run has finished.
2. The sites, as above.
3. The release `demo-books` of this repository (a prerelease, one asset
   `sample-books.tar` of the EPUBs, all CC0 or public domain). The workflow
   downloads it with `gh release download` only when the sites failed, and
   `make sample-books SAMPLE_BOOKS_FROM=…` uses the books that match.

The owner makes or refreshes that copy from their own machine, after
`make sample-books` has fetched and verified every book:

```
make demo-books-release
```

It checks each file in `web/demo/files` against the lock file again, packs
them into `.cache/sample-books.tar` and creates or updates the release (needs
`gh` signed in). Run it again after the lock file changes.

LibriVox audiobooks are not in the list yet: they need a short clip cut with
FFmpeg and a `files` entry with `durationMs`, which a person chooses.

## Books to fetch

Standard Ebooks publishes its EPUBs under CC0 1.0; Project Gutenberg's books
are in the public domain in the United States, and its files carry the
Project Gutenberg licence text, which allows copying.

| Title | Author | Language | Page | Licence |
|---|---|---|---|---|
| Emma | Jane Austen | en | <https://standardebooks.org/ebooks/jane-austen/emma> | CC0 1.0 (Standard Ebooks) |
| Persuasion | Jane Austen | en | <https://standardebooks.org/ebooks/jane-austen/persuasion> | CC0 1.0 (Standard Ebooks) |
| Pride and Prejudice | Jane Austen | en | <https://standardebooks.org/ebooks/jane-austen/pride-and-prejudice> | CC0 1.0 (Standard Ebooks) |
| Sense and Sensibility | Jane Austen | en | <https://standardebooks.org/ebooks/jane-austen/sense-and-sensibility> | CC0 1.0 (Standard Ebooks) |
| A Study in Scarlet | Arthur Conan Doyle | en | <https://standardebooks.org/ebooks/arthur-conan-doyle/a-study-in-scarlet> | CC0 1.0 (Standard Ebooks) |
| The Sign of the Four | Arthur Conan Doyle | en | <https://standardebooks.org/ebooks/arthur-conan-doyle/the-sign-of-the-four> | CC0 1.0 (Standard Ebooks) |
| The Hound of the Baskervilles | Arthur Conan Doyle | en | <https://standardebooks.org/ebooks/arthur-conan-doyle/the-hound-of-the-baskervilles> | CC0 1.0 (Standard Ebooks) |
| The Adventures of Sherlock Holmes | Arthur Conan Doyle | en | <https://standardebooks.org/ebooks/arthur-conan-doyle/the-adventures-of-sherlock-holmes> | CC0 1.0 (Standard Ebooks) |
| Twenty Thousand Leagues Under the Seas | Jules Verne | en | <https://www.gutenberg.org/ebooks/2488> | Public domain (Project Gutenberg) |
| Around the World in Eighty Days | Jules Verne | en | <https://standardebooks.org/ebooks/jules-verne/around-the-world-in-eighty-days/george-makepeace-towle> | CC0 1.0 (Standard Ebooks) |
| Journey to the Centre of the Earth | Jules Verne | en | <https://standardebooks.org/ebooks/jules-verne/journey-to-the-center-of-the-earth/f-a-malleson> | CC0 1.0 (Standard Ebooks) |
| The Time Machine | H. G. Wells | en | <https://standardebooks.org/ebooks/h-g-wells/the-time-machine> | CC0 1.0 (Standard Ebooks) |
| The War of the Worlds | H. G. Wells | en | <https://standardebooks.org/ebooks/h-g-wells/the-war-of-the-worlds> | CC0 1.0 (Standard Ebooks) |
| The Invisible Man | H. G. Wells | en | <https://standardebooks.org/ebooks/h-g-wells/the-invisible-man> | CC0 1.0 (Standard Ebooks) |
| Frankenstein | Mary Shelley | en | <https://standardebooks.org/ebooks/mary-shelley/frankenstein> | CC0 1.0 (Standard Ebooks) |
| Dracula | Bram Stoker | en | <https://standardebooks.org/ebooks/bram-stoker/dracula> | CC0 1.0 (Standard Ebooks) |
| The Picture of Dorian Gray | Oscar Wilde | en | <https://standardebooks.org/ebooks/oscar-wilde/the-picture-of-dorian-gray> | CC0 1.0 (Standard Ebooks) |
| Oliver Twist | Charles Dickens | en | <https://standardebooks.org/ebooks/charles-dickens/oliver-twist> | CC0 1.0 (Standard Ebooks) |
| A Christmas Carol | Charles Dickens | en | <https://standardebooks.org/ebooks/charles-dickens/a-christmas-carol> | CC0 1.0 (Standard Ebooks) |
| Great Expectations | Charles Dickens | en | <https://standardebooks.org/ebooks/charles-dickens/great-expectations> | CC0 1.0 (Standard Ebooks) |
| Jane Eyre | Charlotte Brontë | en | <https://standardebooks.org/ebooks/charlotte-bronte/jane-eyre> | CC0 1.0 (Standard Ebooks) |
| Wuthering Heights | Emily Brontë | en | <https://standardebooks.org/ebooks/emily-bronte/wuthering-heights> | CC0 1.0 (Standard Ebooks) |
| Moby-Dick | Herman Melville | en | <https://standardebooks.org/ebooks/herman-melville/moby-dick> | CC0 1.0 (Standard Ebooks) |
| Alice's Adventures in Wonderland | Lewis Carroll | en | <https://standardebooks.org/ebooks/lewis-carroll/alices-adventures-in-wonderland/john-tenniel> | CC0 1.0 (Standard Ebooks) |
| Through the Looking-Glass | Lewis Carroll | en | <https://standardebooks.org/ebooks/lewis-carroll/through-the-looking-glass/john-tenniel> | CC0 1.0 (Standard Ebooks) |
| The Metamorphosis | Franz Kafka | en | <https://www.gutenberg.org/ebooks/5200> | Public domain (Project Gutenberg) |
| Faust | Johann Wolfgang von Goethe | de | <https://www.gutenberg.org/ebooks/2229> | Public domain (Project Gutenberg) |
| Die Verwandlung | Franz Kafka | de | <https://www.gutenberg.org/ebooks/22367> | Public domain (Project Gutenberg) |
| Die Leiden des jungen Werther | Johann Wolfgang von Goethe | de | <https://www.gutenberg.org/ebooks/2407> | Public domain (Project Gutenberg) |
| Effi Briest | Theodor Fontane | de | <https://www.gutenberg.org/ebooks/5323> | Public domain (Project Gutenberg) |
| Kabale und Liebe | Friedrich Schiller | de | <https://www.gutenberg.org/ebooks/6498> | Public domain (Project Gutenberg) |

## Checking the build

`make web-demo`, then `make test-demo` serves `web/dist-demo` as Pages does
and clicks through it in Chromium (`e2e/demo`): it boots at the base path, a
deep link and a reload, the reader on an EPUB, the player, the "Not in the
demo" pages, and that no request leaves the origin. It is not in the push gate.
