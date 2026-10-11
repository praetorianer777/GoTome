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

## What the catalogue offers

- **Recently added**: the newest books first.
- **Authors** and **Series**, alphabetically; a series's books in their
  order.
- **Shelves**: your collections and smart shelves, and those shared with
  you.
- **Reading** and **Want to read**: the books you have marked so.
- **Search** by title, author or series, from KOReader's search field.

Only books with a file an e-reader reads are listed (EPUB, Kindle formats,
PDF), each with a download per format, the best first, and its cover.
