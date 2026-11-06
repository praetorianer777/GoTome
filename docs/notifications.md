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
| A feed I follow lists a new item I asked for | everyone |
| Details found for a book wait for review | editors and administrators |
| New possible duplicates | editors and administrators |
| Files that cannot be read | editors and administrators |
| A library cannot be scanned | administrators |
| A library's folder cannot be written to (once, until it can again) | administrators |

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

## Following feeds

Publishers and authors announce books in RSS and Atom feeds. On **New
books**, under **Feeds**, give a feed's address and, if you like:

- **Words**, separated by commas: an item must name one of them. None is
  every item.
- **Authors**, separated by semicolons: an item must name one of them.

Each feed is read once an hour, asking the server only for what changed.
What it lists the first time is its past and is not told; after that you
are told once of each new item that names what you asked for, and **From
your feeds** lists them. Each person may follow up to 50 feeds.

A feed that cannot be read says why under its address, and is tried less
and less often: after two hours, then four, up to once a day. After 8
failures in a row it stops until you press **Try again**. Feeds on your
own network (private addresses) are refused, as are answers over 4 MB.
With `GOTOME_OFFLINE=true` no feed is read.
