# A modern genre corpus

Decided in #527, 2026-11-13. A spike: pages were read in a browser-like fetch,
nothing was downloaded and no harness was run.

## Question

The shipped genre model (`genre-detection.md`) is trained on Project Gutenberg,
which has 15 books or more for 25 of the 44 genres. Horror, thriller,
contemporary, classics, dystopia, erotica, young adult, family saga and comics
have fewer, and fantasy, adventure and humour abstain. Is there a labelled corpus
of modern books, English and ideally German, under a licence that allows training
on it and shipping the resulting numbers (weights, never text), with 15 or more
books per missing genre?

## Decision

1. **No source is both modern and free.** Nothing found gives 15 books of a modern
   genre under a licence that allows training and shipping weights. What is
   freely licensed is old (United States public domain: published 1930 or
   earlier in 2026), and what is modern is under a licence that forbids it, is
   non-commercial, or has an unclear origin.
2. **Standard Ebooks is the one source to add to #529, for four genres:**
   horror, fantasy, adventure and humour (its subjects Horror, Fantasy,
   Adventure, Comedy and Satire). They are the genres Gutenberg has too few
   books of, or abstains on, and Standard Ebooks labels its books itself with a
   curated subject and dedicates its own work to the public domain (CC0). Its
   texts are old (gothic horror, pre-1931 fantasy), so this teaches the
   embedding heads; the words do not carry to modern text (`genre-detection.md`,
   why these). The step is conditional: it is taken only if, counted from the
   catalogue, a genre has 15 books Gutenberg's training set lacks (see
   Follow-ups). The expected gain is unmeasured.
3. **The other genres stay with tags and per-library adaptation (#530):**
   contemporary, thriller, young adult, erotica, family saga, comics, and
   dystopia and classics as modern readers mean them. A library's own tagged
   books are worth more than shipped ones in any case (F1 0.73 against 0.66 on
   the five shared genres), and no corpus found changes that.
4. **Labels alone do not help.** Open Library subjects and Wikidata's genre
   property give a genre per title, but the texts of modern titles are not free,
   so there is nothing to train on. They stay what they are: tags that
   `genres.Spellings` maps.

## What was looked at

Counts are from what the pages showed; "est." is an estimate from pagination or
from the kind of source, not a count.

| Source | Licence (quoted) | Languages | Genres and counts | Train and ship weights | Problems |
|---|---|---|---|---|---|
| [Standard Ebooks](https://standardebooks.org/ebooks) | own work "dedicated to the public domain" under CC0 (footer) | English | about 1,400 books (est. from 129 pages of 12; 1,398 on 1 March 2026 per Wikipedia's mirror); 19 subjects incl. Horror (2 to 3 pages of 12, est. 25 to 36), Fantasy, Adventure, Comedy, Satire, Mystery, Science Fiction, Children's; no counts shown per subject | Yes for the edition; the text is public domain in the United States only | Everything is 1930 or earlier; most are also on Gutenberg, so the gain is curated labels and few new books; per-subject counts not read |
| [Project Gutenberg](https://gutenberg.org/policy/terms_of_use.html) (newer additions) | no rule on training; the site "is intended for human users only" | mostly English, some German | the source of #451; additions are also 1930 or earlier (pulp magazines such as Weird Tales and Astounding stories of 1923 to 1930 are there) | Yes in practice, from a mirror, as #451 did | Bulk copy only from mirrors; pulp issues are magazines, not books; no modern books |
| [Projekt Gutenberg-DE](https://www.projekt-gutenberg.org/info/texte/index.html) | "ausschließlich zum privaten Gebrauch gestattet" | German | not stated | No | Private use only; copying, passing on and editing are forbidden |
| [Zeno.org](https://zeno.org/) | terms not read (the page did not answer) | German | not read | Unknown, treated as no | Old texts in any case |
| [Deutsches Textarchiv](https://www.deutschestextarchiv.de/) | CC BY-NC(-SA) 3.0 per secondary sources, which differ (not read at the source) | German | 1,350 to 4,671 texts of 1600 to 1900 in different snapshots; fiction among non-fiction | Doubtful: non-commercial | Old German, which already failed on modern German in #451; NC |
| [Wikisource](https://wikisource.org/wiki/Wikisource:Copyright_policy) (en 1,139,384 content pages, de 657,270) | "public domain or released under CC BY-SA 4.0" | many | genre by category; no count for books by genre | Public-domain works yes; BY-SA ones unsettled for numbers derived from them | Mostly old, mixed with articles, letters and documents; recent works are copyrighted |
| Creative Commons fiction (Unglue.it listings, authors' own sites) | mostly CC BY-NC and BY-NC-SA | English | science fiction in the tens at most; horror and thriller thin (search result, est.) | Non-commercial ones: no | Handful of books by a few authors, one voice per genre; licences differ per title |
| BookCorpus and its replications | scraped from Smashwords | English | 7,185 unique books of 11,038 | No | A paper on it reports "likely violates copyright restrictions" (arXiv 2105.05241) |
| Archive of Our Own | terms page refused the fetch (HTTP 418) | many | fan fiction by tag | No (authors keep their works; not read) | Fan fiction is not the books a library holds |
| [Open Library](https://openlibrary.org/developers/licensing) subjects, Wikidata genre | "does not assert any new copyright" (Open Library) | many | labels for millions of titles, monthly dumps | Labels yes; there is no text | Modern texts are not free; no count of titles with a free text |

Not looked at, for want of time: Internet Archive and HathiTrust full-text
rules, Faded Page, Feedbooks, Wattpad's terms, Baen and Tor's free libraries.
None is known to be both modern and licensed for training; Wattpad and the
publishers' free libraries are free to read, not licensed to copy.

## Why

- **Copyright draws the line at 1930.** A book that is free to copy was
  published before 1931, so a "modern" free corpus does not exist apart from
  the few authors who chose Creative Commons, and they chose non-commercial
  terms and write mostly science fiction.
- **Non-commercial is not usable.** GOtome is open source and self-hosted, but
  its shipped numbers are part of a release; NC-SA material would put its terms
  on them, and nobody here can settle what a trained weight inherits.
- **Old books still teach the heads.** The embedding heads carried from
  Gutenberg to modern German (F1 0.65), so old horror and fantasy are worth
  something where there is none; the words do not carry, so nothing is gained
  for TF-IDF.
- **Standard Ebooks costs little.** Its labels are curated, its licence is
  clear, and the work is a mapping from five subjects to genre keys in the
  training command of #529.

## Limits

- Per-subject counts of Standard Ebooks were not read (the pages show 12 books
  per page, not totals); 25 to 36 for horror is read from the pagination.
- Overlap with Gutenberg is unknown; the same book counted twice would inflate
  the training and the tests.
- Licence texts were read as the pages state them, not as a lawyer reads them.
  Whether weights trained on public-domain texts need any notice is not asked
  of anyone.
- The gain for modern books is a guess. The only test is a library's own tagged
  horror, fantasy and adventure books, which are modern and German.
- Zeno.org and Archive of Our Own could not be read; Wikidata's genre counts need
  a query that was not run.

## Follow-ups to file

1. Count, in the browser, the books per Standard Ebooks subject and the ones not
   already in the Gutenberg training set; the owner fetches the books for
   training (bulk access to be checked against Standard Ebooks' rules first).
2. Extend #529's training command with Standard Ebooks labels for horror,
   fantasy, adventure and humour, deduplicated by title and author against
   Gutenberg, and measure on a library's tagged books of those genres with and
   without it. Keep it only if the held-out F1 of those genres rises.
3. Nothing for the other genres: #530 and the owner's tagging serve them. If a
   genre matters more, ask the owner to tag 100 books of it and let adaptation
   learn it.
