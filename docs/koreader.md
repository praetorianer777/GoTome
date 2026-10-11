# E-readers and KOReader

GOtome is an OPDS catalogue: an e-reader app that reads OPDS, such as
KOReader, browses the library and downloads books from it, signed in with
a token of the device's own.

## Adding a device

1. In GOtome, open your profile and, under **E-readers**, add a device by
   name (say, "Tolino").
2. GOtome shows the catalogue's address, your user name and the device's
   token. The token is shown this once; if it is lost, revoke the device
   and add it again.
3. In KOReader: the search menu, **OPDS catalog**, the **+** at the top,
   and enter
   - **Catalog name**: GOtome
   - **Catalog URL**: the address shown, such as
     `https://books.example.org/api/v1/opds`
   - **Username**: your user name
   - **Password**: the token, as shown or in capitals.

The device sees the books you see and nothing else of your account: its
token opens the catalogue and downloads, not the rest of GOtome. Revoking
the device on your profile signs it out for good. Wrong tokens are refused
for a while after five tries, as wrong passwords are.

Over the internet, put GOtome behind HTTPS: the token travels with every
request as the password of HTTP Basic auth.

## Sending books to a device

On a book's page, **Send to an e-reader** puts the book in the device's
queue; in the library, select books and send them all at once. The
catalogue's first entry on the device, **Sent to this device**, lists the
books waiting there: download one and it leaves the queue, and its page in
GOtome says since when the device has it. A book not yet downloaded can be
taken back from its page. Sending a book the device has already queues it
again.

The KOReader plugin (#197) will fetch what was sent by itself when Wi-Fi
comes up; until then, open **Sent to this device** in the catalogue.

## What the catalogue offers

- **Sent to this device**: the books sent here, until they are downloaded.
- **Recently added**: the newest books first.
- **Authors** and **Series**, alphabetically; a series's books in their
  order.
- **Shelves**: your collections and smart shelves, and those shared with
  you.
- **Reading** and **Want to read**: the books you have marked so.
- **Search** by title, author or series, from KOReader's search field.

Only books with a file an e-reader reads are listed (EPUB, Kindle formats,
PDF), each with a download per format, the best first, and its cover.
