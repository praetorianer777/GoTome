# Benchmark corpus

The search spike (#14) and the embedding spike (#15) measure on real books at two
scales. This tool builds both corpora under `.cache/corpus/`, which is ignored by
git and excluded from the image build context. Nothing here runs in the gate.

```bash
make corpus          # base corpus: about 2,000 public-domain EPUBs
make corpus-50k      # the base corpus replicated to 50,000 books
make corpus-stats    # composition of the base corpus
make corpus-test     # the tool's own tests, offline
```

## Base corpus

Source is [Project Gutenberg](https://www.gutenberg.org), EPUB without images.
Default composition, changeable with `CORPUS_LANGS=code:count,…`:

| Language | Books |
|---|---|
| English (`en`) | 1,200 |
| German (`de`) | 600 |
| French (`fr`) | 200 |

English and German are the two languages the search spike has to stem; French is
there so language detection has a third case. Books are taken in the order the
harvest lists them. `fetch` prints the actual counts and sizes when it finishes,
and `make corpus-stats` prints them again.

Layout: `.cache/corpus/base/<lang>/pg<id>.epub`.

### Access rules

Project Gutenberg blocks robots on its website and offers one entry point for
them, the [harvest endpoint](https://www.gutenberg.org/policy/robot_access.html),
with a pause of two seconds between requests. The tool uses only that endpoint and
the mirror links it returns, identifies itself in the user agent, and refuses a
shorter pause. At that pace the default corpus takes a little over an hour.

A run can be interrupted and started again: books already on disk count towards
the quota. A file that is not an EPUB is skipped and reported.

## 50k corpus

`replicate` copies the base corpus as often as it takes to reach `CORPUS_COUNT`
books (default 50,000). Each copy gets ` (copy N)` appended to its first title and
`-copy-N` to its first identifier, so an importer sees distinct books with distinct
file hashes. Everything else in the file is copied byte for byte.

Layout: `.cache/corpus/50k/<lang>/pg<id>-<NNN>.epub`.

What this corpus is good for, and what not:

- Index size, build time, memory and query latency at 50,000 books: yes.
- Vocabulary growth: no. The 50k corpus has the vocabulary of the base corpus,
  so term dictionaries are smaller than in a real library of that size.
- Duplicate detection: no. Every book has 24 near-identical siblings by design.

Disk use is 25 times the base corpus. `fetch` prints the size of the base corpus;
check the free space before running `make corpus-50k`.
