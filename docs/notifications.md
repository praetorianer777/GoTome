# Notifications and new books

## Notifications

GOtome tells people of what happens in their libraries in the app: the
bell in the header counts what is unread, and opens the list. There are no
e-mails or messages to other services yet.

What a person is told of depends on their role, and each person chooses on
their profile, under **Notifications**, which of these they want. All are
on until turned off.

| On the profile | Who can get it |
|---|---|
| New books in a library (once per scan) | everyone |
| A book on my wishlist arrives | everyone |
| A new book of an author or series I follow is announced | everyone |
| A book of an author or series I follow comes out | everyone |
| Details found for a book wait for review | editors and administrators |
| New possible duplicates | editors and administrators |
| Files that cannot be read | editors and administrators |
| A library cannot be scanned | administrators |

Nobody is told of a library they do not see. Many events of one kind
within a few minutes, such as an import of a thousand books, are told
once, as a count. An e-reader asking to be added to someone's account
tells them too ([devices.md](devices.md)).

## Following authors and series

On a book's page, **Follow for new books** follows its authors and its
series; **New books** in the navigation lists what you follow, lets you
follow an author or series by name, and shows their books:

- **Coming**: books announced and not out yet.
- **Out in the last year**.

Each book is marked **In your library** where a library you see has it
already.

GOtome asks the sources once a day about each subject someone follows
(Hardcover for authors and series, Open Library for authors), so a
Hardcover token ([metadata.md](metadata.md)) makes series work. Books in
the language of `metadata.language`, in English, and those of no stated
language are kept.

What a source lists the first time a subject is looked up is its past:
it is shown, but not told as news. After that you are told when a book is
announced, and again when it comes out, unless a library you see already
has it. Several at once come as one notification.

With `GOTOME_OFFLINE=true` nothing is looked up.
