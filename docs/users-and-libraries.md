# People, roles and libraries

## Roles

Every account has one of three roles. Each role can do everything the one
before it can.

| Role | Can |
|---|---|
| **Reader** | Browse, search, read, listen to and download the books of the libraries they see; keep their own reading status, ratings, progress, collections, smart shelves, wishlist and followed authors and series; add their own e-readers. |
| **Editor** | Also upload books; edit details and covers, look books up with the metadata sources and review matches; merge duplicates and clean up names; move files to the trash and restore them; fix EPUBs for e-readers; scan libraries, read files again and rebuild the search index; see the jobs. |
| **Administrator** | Also manage accounts and their upload quotas, add, change and remove libraries, change the settings, delete files from the trash for good, and see the "This machine" panel. Administrators see every library. |

The navigation shows each person only the pages their role opens.

What is personal stays so: someone's reading status, rating, progress,
statistics and private collections are theirs alone. Administrators do
not see them either.

## Accounts

Administration, **Users**:

- **Add a user**: a user name, a first password (at least 8 characters,
  which they can change on their profile) and a role.
- **Role**: changed at any time. GOtome refuses to leave no active
  administrator.
- **Disable**: the account can no longer sign in, and its sessions and
  devices stop working at once. **Enable** lets it in again.
- **Set a new password**: for someone who forgot theirs. It signs them out
  everywhere.
- **Sign out everywhere**: ends every session of the account.
- **Upload quota in GB**: how much a person's uploads may take up. Empty is
  no limit. Each person sees on their profile how much they use.

With single sign-on, accounts can also be made when someone first signs
in, and their roles taken from the identity provider's groups
([single-sign-on.md](single-sign-on.md)).

Each person's profile has their password, their sessions (to end one on a
lost device), their e-readers, their notifications and, with single
sign-on, the account at the identity provider linked to theirs.

## Libraries and who sees them

A library is a folder of books, and the boundary of who sees what. Every
book belongs to exactly one library.

A library is either:

- **Shared** ("Everybody"): every signed-in person sees it, or
- **Private** ("Only its members"): only the administrator who made it,
  the people made its members, and administrators see it.

A book in a library someone does not see does not exist for them,
everywhere in GOtome: not in lists, counts, search, similar books,
duplicates, notifications, collections someone else shared, the OPDS
catalogue or their devices. Asked for by its address, it answers exactly
as a book that does not exist. A collection or shelf never shows more
than its viewer may see.

Members of a private library are added and removed through the API
(`PUT` and `DELETE /api/v1/libraries/{library}/members/{user}`, as an
administrator); the Libraries page does not show them yet.

### Managed and existing folders

| | Managed by GOtome | An existing folder |
|---|---|---|
| Where | `/data/libraries/<id>` in the data volume, or a path you give | a folder mounted into the app container |
| How books arrive | uploads (**Add books**) | you put files into the folder; the next scan finds them |
| Arranged by | GOtome: a folder per book, named after it | you |
| GOtome writes there | always | only with **GOtome may change files here**, and a mount without `:ro` |
| In a backup | yes | no: back up the folder yourself |

What GOtome writes into a library it may change:

- edited details and covers, into each EPUB's metadata, so that other
  apps see them too; the book's text and every other part of the file
  stay as they were;
- **Move to trash**, which moves the file into the library's `.trash`
  folder, from where **Restore** brings it back until it is deleted for
  good (after `trash.retentionDays`, 30 days by default);
- **Fix** for e-readers, which replaces an EPUB with a lighter one and
  moves the original into the trash.

GOtome checks that it can write to the folder when **GOtome may change
files here** is turned on, by making a dot file there and removing it
again: a folder mounted with `:ro`, or one the app's user 10001 may not
write to, is refused with the reason and the folder's path. It checks
again at every scan of a writable library. Should the folder have become
read-only since (a mount changed, a disk full or remounted read-only),
the library's line on the Libraries page says so and since when, and
administrators are notified once ("A library's folder cannot be written
to"), not once per file. Until a scan finds the folder writable again,
edits are kept in GOtome but not written into its files, **Move to
trash**, **Restore** and **Fix** are refused with the reason, and its
trash is not emptied. An edit made in that time reaches the file with
the book's next edit.

Without that permission GOtome only reads: a file that disappears from
the folder is marked missing, and nothing on disk is moved, renamed or
deleted. An upload of a file whose exact bytes are already in a library
the uploader sees is not stored again: GOtome points to the book that
has it.

**Remove** on the Libraries page removes a library and its books from
GOtome, with everything people kept about them. The folder and its files
stay on disk.

### Scanning

A scan looks through a library's folder for new, changed, moved and
missing files. A file whose size and time have not changed is not read
again; a moved file is recognised by its content and keeps its book.
Folders whose names start with a dot are passed by.

Libraries are scanned when they are added, every `GOTOME_SCAN_INTERVAL`
(six hours by default), and when an editor presses **Scan now**. A scan
of a large library works in turns and goes on after a restart.
