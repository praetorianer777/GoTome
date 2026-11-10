# Resolving duplicates by themselves

GOtome finds pairs of books that look like one (the same file, the same
text, an ISBN or a title and author in common) and lists them under
**Duplicates** for a person to decide. For the pairs where the evidence
leaves no doubt it can do the deciding itself, in a run you start or every
day, and every run can be looked at and undone under **Changes**.

Nothing is switched on by default.

## The classes

A pair's class comes from the evidence it was found with. The text figures
are the Jaccard similarity of the two books' text (how much of all their
words they share) and how much of each book the other holds.

| Class | Setting name | Evidence | What the rule does |
|---|---|---|---|
| Same file or same text | `same_text` | A file of each with the same bytes or the same content | Merge |
| Nearly the same text | `overlap_high` | Shared text, Jaccard 0.95 or more | Merge |
| Mostly the same text | `overlap_mid` | Shared text, Jaccard 0.8 to under 0.95 | Merge, once its numbers have been looked at |
| One holds the other | `contained` | One book holds 0.95 or more of the other, Jaccard under 0.5 | Keep both, link |
| Part inside | `part_inside` | One holds 0.8 to under 0.95 of the other, Jaccard under 0.5 | Keep both, link |

Every other pair stays for a person: shared text of a Jaccard from 0.5 to
under 0.8, an ISBN or a title and author without shared text, and a pair
that holds shared text and also an ISBN while one book holds the other.
Another proof of identity (an ISBN, the same title and author) beside a
Jaccard of 0.8 or more only agrees with the text and does not stop a merge.

Only pairs of two books of one library, in libraries the person who starts
the run sees, are touched. A pair with an audiobook is left alone: it may be
another recording.

## What a merge does

- **The surviving book** is, in this order, the one anyone has a reading
  status, rating, shelf place or position with; the one with a field a
  person set or locked; the one with the better format (EPUB over MOBI and
  AZW3 over PDF); the one with more details filled in; the older.
- **The fields** are decided one by one by the same rules as clean-up and
  matches use (docs/metadata.md): a value present on one side only is taken;
  the longer of two descriptions that contain each other; a locked or typed
  value over an automatic one; authors, series and publishers by the
  spelling most books of the library use. A field no rule decides is a
  *conflict*. The setting `curation.duplicates.onConflict` says what then:
  `skip` (the default) leaves the whole pair to a person; `mergeAgreeing`
  merges the pair, taking what agrees, and the surviving book keeps its own
  value of the fields in conflict. Locked fields of the surviving book are
  never overwritten.
- **The files.** Where each book has exactly one file of an ebook format and
  both are of the same format, the better one is kept (the one without
  findings for e-readers, then the newer) and the other goes to the trash, as
  a replace does. Files of formats only one book has, and the parts of an
  audiobook, are kept. Where the library cannot be written to, both files
  stay.
- Everything else (reading status, places, shelves, deliveries, notes)
  moves to the surviving book as in a manual merge.

A run is an *automatic* run: the files it trashed stay in the trash, and
nothing is written into files, until a person confirms it under **Changes**.
Undoing a run is a job too: the page shows how far it has got, and an action
whose books have been changed since is left as it is and says so.

## What linking does

For the two link classes the pair is set to *kept both*, and a relation is
recorded between the books (`contains` for the one that holds the other's
text, `contained_in` for the other). Nothing moves. A pair that a person
has related already is left as it is.

## Running

- On the **Duplicates** page each class has its count of open pairs and the
  buttons **Dry run** and **Run**. A dry run goes through the pairs and records
  what it would do, with the reason, and changes nothing; it shows under
  **Changes** with a *Dry run* mark and cannot be undone or confirmed. **Run**
  asks first, with the number of pairs.
- The setting `curation.duplicates.classes` lists the classes (separated by
  commas) the rules handle by themselves. Empty, the default, handles none.
  Once set, a daily run handles the open pairs of those classes, the pairs
  found since the last run among them. A pair that was left because of a
  conflict is looked at again each day.
- Over the API: `POST /api/v1/curation/duplicates` with `classes` and
  `dryRun` (needs `metadata:edit`) queues the run and answers 202 with its ID;
  `GET /api/v1/curation/duplicates` counts the open pairs per class.
  One run goes at a time.

## Measuring

`gotome duplicate-check` takes a sample of the open pairs of each class (by a
hash of the pair's ID; `-n` at most, 100 by default) and prints what the rules
would do with them: how many would be merged, how many of those trash a copy,
how many linked, how many left and why, which book would survive and why,
which fields would be taken, and which differ. It prints counts, field names
and rule names, never a title or an author, and changes nothing. Use it
before switching a class on, and especially before `overlap_mid`:

```
docker compose exec app gotome duplicate-check -n 200
docker compose exec app gotome duplicate-check -on-conflict mergeAgreeing
```

## The numbers

On a library of about 48,500 books with 2,369 open pairs, all within one
library and none of two formats, the pairs fell in the classes like this:

| Class | Open pairs |
|---|---|
| `same_text` | 86 |
| `overlap_high` | 830 |
| `overlap_mid` | 161 |
| `contained` | 556 |
| `part_inside` | 328 |
| Shared text of a Jaccard from 0.5 to under 0.8 | 179 |
| An ISBN or a title and author without shared text | about 57 |

`overlap_mid` and `part_inside` are meant to be switched on only from the
numbers of `gotome duplicate-check` on your own library: whether the sample's
survivors and fields are right.
