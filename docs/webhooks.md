# Webhooks

A webhook tells another service when something happens in GOtome: a scan
brought in books, a scan failed, duplicates were found, a book you follow
came out, a newer GOtome is available. Administrators set them up under
**Webhooks** in the navigation (the `settings:manage` permission).

## Setting one up

- **Address**: an `http://` or `https://` address, without a user name.
  Any token the receiver wants can be part of the address.
- **Events**: the kinds to send; none ticked is every kind. They are the
  kinds on the profile's notification list, such as `books.added`,
  `scan.failed`, `library.unwritable`, `files.unreadable`,
  `duplicates.found`, `review.needed`, `wish.fulfilled`, `release.announced`,
  `release.out`, `system.degraded`, `update.available` and `feed.item`.
- **Secret**: 16 to 200 characters that only GOtome and the receiver know,
  such as the output of `openssl rand -hex 32`. It is kept sealed and never
  shown again; leave the field empty when you change a webhook to keep it.
- **Trusted**: GOtome reaches only public addresses, as it does for every
  request it makes to another server; an address inside your network
  (`http://homeassistant.local:8123/...`, a container on the same host) is
  refused. Tick **Trusted** for such an address. It trusts that host and port
  and nothing a redirect names, and GOtome does not follow redirects for
  webhooks at all.
- **Send test** queues an event of kind `webhook.test` to this webhook, off
  or on.

## What is sent

One event is sent once to every webhook that wants its kind, however many
people are notified of it. The body is JSON:

```json
{
  "kind": "books.added",
  "time": "2026-11-12T09:30:00Z",
  "libraryIds": ["0195a1c2-..."],
  "bookId": "0195a1c3-...",
  "link": "https://books.example.org/?library=0195a1c2-...",
  "data": {"count": 12, "library": "0195a1c2-..."}
}
```

Only IDs, counts, versions and a link are sent: never a title, an author, a
file name, an error text or any text of a book. `link` is absolute when
`GOTOME_PUBLIC_URLS` names an address, else it is a path on your server.
`data` holds, where the event has them, `count`, `library`, `file`,
`component`, `version`, `current`, `security`, `date` and `precision`.

The request is a `POST` with `Content-Type: application/json` and these
headers:

| Header | Holds |
|---|---|
| `X-GOtome-Event` | the kind, as in the body |
| `X-GOtome-Delivery` | the ID of the delivery; the same on every retry |
| `X-GOtome-Timestamp` | the time of this attempt, in Unix seconds |
| `X-GOtome-Signature` | `sha256=` and the hex HMAC-SHA256 of the timestamp, a full stop and the raw body, keyed by the secret |

## Checking a delivery

Compute the HMAC over `timestamp + "." + body` with the secret, compare it
with the header in constant time, and refuse a timestamp more than a few
minutes from your clock: the timestamp is part of what is signed, so a
captured request cannot be sent again later. In Python:

```python
import hmac, hashlib, time

def valid(secret: bytes, timestamp: str, body: bytes, signature: str) -> bool:
    if abs(time.time() - int(timestamp)) > 300:
        return False
    mac = hmac.new(secret, timestamp.encode() + b"." + body, hashlib.sha256)
    return hmac.compare_digest("sha256=" + mac.hexdigest(), signature)
```

Answer with any `2xx` status; the body is ignored. Answer quickly (GOtome
waits 15 seconds) and do the work afterwards.

## Retries and the log

A delivery that fails (a network error, a status outside 2xx) is tried again
after 30 seconds, then 2 and 8 minutes, then after 32 minutes, 2 hours,
9 hours and 34 hours: eight attempts in all, then it is marked failed. Each
attempt carries a new timestamp and signature, the same delivery ID and body,
so the receiver should treat a delivery ID it has seen as done.

**Deliveries** on a webhook's row lists the latest hundred with their state,
attempts, the status the receiver answered, the last error and what was
sent. Deliveries older than 30 days, and all but the latest 200 of a
webhook, are removed. A backup keeps the webhooks (their secrets stay sealed
with `secret-key`) and leaves the log out.
