# On a phone and offline

GOtome's web app installs on a phone or a computer like an app, and keeps
books you choose in the browser so that you can read or listen without a
connection.

## Install it

The browser needs a secure address: HTTPS (see the reverse proxy in
[getting-started.md](getting-started.md)), or `localhost`. On a plain
`http://` address on your network the app works, but cannot be installed or
keep books.

- **Android (Chrome)**: menu, **Install app** (or **Add to Home screen**).
- **iPhone and iPad (Safari)**: share button, **Add to Home Screen**.
- **Desktop (Chrome, Edge)**: the install icon in the address bar.

The installed app opens in its own window. Its files (the app itself, not
your books) are kept by the browser, so the app opens even when the server
cannot be reached; it then shows only what you have kept.

## Keep a book offline

On a book's page, each ebook, PDF or audio file has **Keep offline**; an
audiobook in several parts also has **Keep all audio offline**. The file is
copied into this browser, together with the book's details, your place in
it and its cover. The book then opens in the reader or the player with no
connection.

Your profile, under **Offline**, lists what is kept, the space each file
takes and the space this browser allows the site, and removes a file or
everything. A browser may clear a site's storage when the device runs short
of space; keep the book again if it was.

A file is a copy of the file as it was when you kept it. If the file is
changed on the server later (a fix for an e-reader, say), remove the copy
and keep it again.

## Places read offline

A place you reach offline is remembered in the browser and sent when the
connection is back, as it would have been. The rule is the same as for any
place: if another device saved a further place in the meantime, that one
stays and the reader offers it the next time you open the book. Your
profile says how many places wait.

## What is stored, and when it goes

- The service worker keeps the app's files, never an answer of the API.
- For a kept book, and only for it, the browser keeps the answers the
  reader or player asks for: the book's details, your place in it and its
  cover, with who you are (the app needs that to open).
- Signing out removes every kept book, answer and place not yet sent from
  the browser, and so does signing in as another person.
- Nothing is sent anywhere but to your GOtome server, and the content
  security policy is unchanged.

Under a sub-path ([reverse-proxy-sub-path.md](reverse-proxy-sub-path.md))
the worker's scope is that path; two GOtome installations on one host name
keep their books apart.

Lock-screen controls of the audiobook player (play, pause, skip, seek, title
and cover) use the browser's Media Session; a browser that has none plays
without them.
