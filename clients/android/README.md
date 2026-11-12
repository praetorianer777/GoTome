# GOtome for Tolino

A companion app for Tolino e-readers (Shine 4 and others on firmware 16.x,
Android 8.1). Reading stays in the Tolino's own reader; the app connects it to
GOtome:

- **Send to device**: books you send to the Tolino from GOtome's web UI
  ("Send to an e-reader" on a book or a selection) land in the Tolino library
  the next time the reader is online.
- **Store**: browse and search your GOtome library on the Tolino, nine books a
  page, and put a book on the Tolino with one tap. Pages come from GOtome as
  you turn them, so the store needs Wi-Fi; the books on the Tolino are listed
  without it.
- **Reading progress**: how far you read on the Tolino shows up in GOtome, and
  the web reader opens the book there.

- **Reading on from elsewhere**: when you open a book on the Tolino that you
  read further in elsewhere, for instance in GOtome's web reader, a bar offers
  to jump there; on "Jump" the app enters the page in the reader's own "Go to
  page".

## How it works

| Part | How |
|---|---|
| Books arrive | The app saves them under `Books/GOtome/`. The Tolino reader imports whatever appears under `Books/` by itself within seconds. |
| Progress | The Tolino reader writes every page turn to the system log. The app reads only those lines (the tags `HomeActivity$DeliverableClickListener`, `EpubViewerActivity`, `EpubView` and `BasicViewerActivity`) for the EPUBs it delivered. The reader logs its position in Adobe's pages, one for every 1024 bytes of each chapter as compressed in the EPUB; the app counts them in the file and sends the share of the book as `fraction:0.42`. PDFs report no progress. |
| Store from the home screen | The app puts a book "GOtome" into the library. When the log shows it opened, the app brings the store up over it. Deleted, it comes back with the next sync or when the store is opened. |
| Jump | The reader takes no position from other apps, but its page numbers ("13 / 510") are Adobe's pages counted from 1. When a book opens, the app asks GOtome for its position (or, offline, goes by the last sync); further by more than a page, an accessibility service shows the offer and, on "Jump", opens the reader's menu, taps the page number and enters the page, by the reader's view IDs. It acts in the Tolino reader only, and only on your tap. |
| Signing in | As a device: it asks to be added to your account and you accept it on your profile, or you type a token made there. The token reaches only what a device needs (the books, their files and covers, your reading progress, and the books sent to it); revoking it signs the Tolino out. |
| On the device | GOtome is told which books the app put on the Tolino and which left it, so a book's page in the web says "On Tolino since …". |
| Sync | Whenever the Tolino goes online (at most every five minutes), once an hour, and whenever a book is closed; "Sync" in the store does it at once. |

The app cannot go online by itself: the Tolino turns Wi-Fi off when idle and
disables its saved networks with it, and only the Tolino's own app may enable
them again. Turn Wi-Fi on in the Tolino's quick settings to sync; books
reach the device the next time it is online for any reason.

The log lines are debug output of the Tolino reader, not an interface. If a
firmware update changes them, books still arrive but progress stops; the
settings page then says it has seen no page turn.

The system log also holds other apps' lines, among them the Thalia shop's
sign-in while the shop is open. The app asks logcat for the four tags above
only and keeps nothing but the book's path and its position.

## Install

You need a computer with `adb` (Android platform tools) and a USB cable.

1. **Developer mode on the Tolino**: type the developer code into the search
   field of the library (on firmware 16.x see the Tolino community for the
   current code), open the developer options that appear and turn on
   **USB debugging**.
2. Connect the Tolino and allow the computer on its screen ("Allow USB
   debugging?", tick "Always allow").
3. In a checkout of GOtome:

   ```bash
   make android-install FROM=release   # the latest release; TAG=v0.2.0 for another
   make android-install                # or a debug build of the checkout
   ```

   This downloads the release's APK with `gh` (or builds the app in a
   container), installs it, grants it the log
   (`pm grant … android.permission.READ_LOGS`, which only adb can grant) and
   storage, and opens its settings.
   It also turns the jump service on (an accessibility service, added to any
   already on). With an APK from elsewhere it is:

   ```bash
   adb install -r gotome-tolino.apk
   adb shell pm grant io.github.praetorianer777.gotome.tolino android.permission.READ_LOGS
   adb shell pm grant io.github.praetorianer777.gotome.tolino android.permission.READ_EXTERNAL_STORAGE
   adb shell pm grant io.github.praetorianer777.gotome.tolino android.permission.WRITE_EXTERNAL_STORAGE
   adb shell settings put secure enabled_accessibility_services io.github.praetorianer777.gotome.tolino/.JumpService
   adb shell settings put secure accessibility_enabled 1
   adb shell appops set io.github.praetorianer777.gotome.tolino REQUEST_INSTALL_PACKAGES allow
   ```

   (The `settings put` replaces other accessibility services that are on;
   `make android-install` adds to them.)

4. On the Tolino, in the app's settings:
   - **GOtome address**: how the Tolino reaches GOtome, e.g.
     `https://gotome.example.org` or, on your own network,
     `http://192.168.1.10:8080` (not `localhost`). Over `http://` the token
     travels unencrypted, which the settings page points out.
   - **User name** of your GOtome account.
5. **Request access**. The Tolino shows a four-digit code. In GOtome, on your
   profile under **E-readers**, the Tolino asks to be added (a notification
   says so too): **Accept** it if it shows the same code. Within a few
   seconds the Tolino signs in by itself, fetches what was sent to it, puts
   the "GOtome" book into the library and starts following the reader. The
   settings page should say that it may read the log. A request nobody
   accepts ends after ten minutes.

   Instead of asking, **Use a device token instead** takes a token made on
   the profile with **Add a device**, typed in with the user name.

USB debugging can be turned off again afterwards; the app keeps its
permissions.

## Use

- **Put a book on the Tolino from the web**: "Send to an e-reader" on its
  page, or on a selection in the library, and choose the Tolino. It appears
  on the Tolino when it is next online (at the latest within an hour, at once
  with "Sync" in the store).
- **From the Tolino**: open the book "GOtome" in your library, or the app from
  a launcher such as Simple Ink Launcher. Search, turn pages with ◀ ▶ or a
  swipe, tap a book, "Put on Tolino". GOtome's page of the book then says it is
  on the Tolino.
- **Reading progress**: just read. Books you put on the Tolino through GOtome
  report their position; books copied by other means or bought in the
  Tolino's shop do not, and GOtome does not know they are on the device.

Deleting a book in the Tolino library deletes its file, and GOtome is told.
Sending it again, or "Put on Tolino" in the store, brings it back.

Coming from a version that signed in with a password and used the "Tolino"
collection as the queue: sign in again with a device token. The books already
on the Tolino stay and are told to GOtome; the collection is left as it is,
and adding books to it no longer sends them.

## Updates

Each GOtome release carries the app as `gotome-tolino-<version>.apk`, signed
with the release key. An installed release looks for a newer one with a sync,
at most once a day, or at once with **Check for updates** on the settings
page (⚙); the store's status line then says so, and **Update to …** there
downloads it and hands it to Android's installer,
which asks once. Sign-in, the books on the Tolino and the positions stay.

The app installs an update only when it is signed with the same key as the
app itself. A build from `make android-install` is signed with the debug key
of your checkout, so it offers a release but cannot take it: switch with
`make android-uninstall`, then `make android-install FROM=release`, and sign
in again. Without a checkout, to install a release the first time:

```bash
adb install gotome-tolino-0.2.0.apk
adb shell appops set io.github.praetorianer777.gotome.tolino REQUEST_INSTALL_PACKAGES allow
```

and the `pm grant` and `settings put` lines of the install steps.

For the owner: release.yml builds the APK with `make android-release`, from
the repository secrets `TOLINO_KEYSTORE` (the release keystore, base64) and
`TOLINO_KEYSTORE_PASSWORD`; the key's alias is `gotome-tolino`. Without them a
release fails rather than publish an APK signed otherwise.

## Uninstall

```bash
make android-uninstall   # or: adb uninstall io.github.praetorianer777.gotome.tolino
```

This also removes the app's settings (address, user name, token) and its
record of which books it put on the Tolino. The books under `Books/GOtome/`
and the "GOtome" book stay in the Tolino library until you delete them there.
Revoke the token on your profile in GOtome if the Tolino is not to sign in
again.

## Development

```bash
make check-android   # unit tests and a debug build, in the container
make android-apk     # the APK, path printed at the end
make android-install # build, install over adb, grant the log
make android-download # only download the latest release's APK (or TAG's)
```

Without a cable: with the cable in once, `adb tcpip 5555`, then
`adb connect <tolino-ip>:5555` and `ANDROID_SERIAL=<tolino-ip>:5555 make
android-install`. It lasts until the Tolino restarts, and works only while its
Wi-Fi is on.

`app/src/test/resources/tolino-16.2.1.log` holds the reader's lines recorded on
a Shine 4 with firmware 16.2.1; a firmware that logs differently gets a
recording of its own next to it.
