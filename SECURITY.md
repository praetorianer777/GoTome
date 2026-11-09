# Security policy

GOtome holds people's libraries, reading history and, for administrators,
the keys of identity providers and metadata services. A weakness in it
matters, and a report of one is welcome.

## Supported versions

Fixes go into the next release of the latest minor version. Before 1.0 there
are no back-ports to older versions: upgrade to the latest release
([docs/backup-and-upgrade.md](docs/backup-and-upgrade.md)).

## Reporting a vulnerability

Please do not open a public issue, discussion or pull request for a
vulnerability.

Report it privately through GitHub:
[**Report a vulnerability**](https://github.com/praetorianer777/GoTome/security/advisories/new)
(the Security tab of this repository). Say what is affected, how to reproduce
it, and what an attacker gains; a proof of concept against your own
installation helps. Never test against someone else's installation.

What happens then:

- An answer within a week, saying whether the report is accepted.
- A fix in a release as soon as it is ready, usually within 30 days for
  anything serious.
- A GitHub security advisory and a CVE where it warrants one, crediting you
  unless you prefer otherwise.
- The release's notes name the fix under **Security** in
  [CHANGELOG.md](CHANGELOG.md). Security fixes are never hidden among other
  changes.

## In scope

The server (`backend/`), the web app (`web/`), the full-text engine
(`engine/`), the container image and compose files (`deploy/`), the KOReader
plugin and the Tolino app (`clients/android`). Examples: reaching a book,
library or account one may not see; signing in as someone else; a crafted
book file that escapes the limits on what is read (zip bombs, FFmpeg,
poppler); a request the server makes to an address an attacker chooses.

Out of scope: installations that disable the protections the documentation
asks for (for example a public server without HTTPS), denial of service by
sheer volume, and vulnerabilities in dependencies that GOtome does not reach.

## How GOtome is made

Most of GOtome's code is written by AI agents (Claude) under the direction of
one maintainer, and every change passes the same gate: the push gate
(`./run-tests.sh`), CI in four browsers, the visibility tests that every
route must pass, scale, backup and upgrade tests. That makes careful reports
from people more valuable, not less.
