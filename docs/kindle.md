# Send to Kindle

GOtome can e-mail a book to a Kindle. Amazon's "Send to Kindle by e-mail"
takes the file, makes it Kindle-ready and puts it in the person's library.

## What is sent

- **EPUB files, as they are.** Nothing is converted. A book with no EPUB is
  not sent; GOtome says so.
- **At most 50 MB**, the limit Amazon gives for a message. A larger EPUB is
  refused when you ask for it. Your mail server may allow less (25 MB is
  common); it then refuses the message and the send fails with its answer.
- Only to an address at `kindle.com` (a person's own Kindle address, such
  as `name_1234@kindle.com`). GOtome writes to no other address.

## Setting it up (administrators)

On the Settings page, under **Mail**, give GOtome a mail server to send
through: `mail.host`, `mail.port`, the encryption, a user name and password
if the server wants them, and the sender address (`mail.sender`). See the
[configuration reference](configuration.md#mail-send-to-kindle).

- The server is the only host GOtome ever sends mail to. Because you named
  it, it may be on your own network.
- The connection is encrypted: STARTTLS (the default, port 587) or TLS (port
  465). If the server does not offer STARTTLS, nothing is sent and the
  password is not offered. `none` turns encryption off and is meant for a
  server on a network you trust; a password is not sent in the clear to any
  host but `localhost`.
- The password is kept sealed like the other secrets, never shown again and
  never logged.
- Without `mail.host` and `mail.sender` the feature is off, and so it is with
  `GOTOME_OFFLINE=true`.

## Setting it up (everyone)

1. In your Amazon account, under **Content & Devices**, **Preferences**,
   **Personal Document Settings**, add the **sender address** to the
   *Approved Personal Document E-mail List*. GOtome shows the address on your
   profile and on each book. Amazon silently drops mail from any other
   sender.
2. Copy your Kindle's e-mail address (Content & Devices, Devices, your
   Kindle) into **Send to Kindle** on your profile.

## Sending

On a book's page, **Send to Kindle**; in the library, select books and use
**Send to Kindle** in the bar above the list. Each book is its own job: its
outcome is on the book's page, and a notification tells you when it was sent
or why it was not. A server that cannot be reached, or asks to wait, is tried
again a few times.

A book that is waiting to be sent is not queued twice, and a send is never
repeated by itself: if GOtome stops while one is being handed to the mail
server, it is marked as failed and you decide whether to send it again.
Sending an already sent book sends it again.

## When it does not arrive

- The notification quotes the mail server's answer. "Sender not approved" or a
  refused recipient is a wrong address, or the approved-sender step above.
- A send that went through GOtome's mail server but never reached the Kindle
  is, nearly always, the sender missing from Amazon's approved list.
