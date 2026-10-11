# GOtome for Tolino

A companion app for Tolino e-readers (Shine 4 and others on firmware 16.x,
Android 8.1). Reading stays in the Tolino's own reader; the app connects it to
GOtome:

- **Send to device**: books you put in your collection "Tolino" in GOtome's
  web UI land in the Tolino library the next time the reader is online.
- **Store**: browse and search your GOtome library on the Tolino, nine books a
  page, and put a book on the Tolino with one tap. The catalogue is kept on the
  device, so browsing does not wait on Wi-Fi.
- **Reading progress**: how far you read on the Tolino shows up in GOtome, and
  the web reader opens the book there.

What it cannot do: move the Tolino reader to a position read elsewhere. The
Tolino keeps its positions to itself; the store's book page shows where GOtome
says you are ("GOtome: 47 % read elsewhere"), and you turn there with the
reader's "Go to page".

## How it works

| Part | How |
|---|---|
| Books arrive | The app saves them under `Books/GOtome/`. The Tolino reader imports whatever appears under `Books/` by itself within seconds. |
| Progress | The Tolino reader writes every page turn to the system log. The app reads only those lines (the tags `HomeActivity$DeliverableClickListener`, `EpubViewerActivity`, `EpubView` and `BasicViewerActivity`) for the books it delivered, and sends the position as `fraction:0.42`. |
| Store from the home screen | The app puts a book "GOtome" into the library. When the log shows it opened, the app brings the store up over it. |
| Sync | When Wi-Fi is on, once an hour and whenever a book is closed. The store's "Sync" button turns Wi-Fi on if it is off. |

The log lines are debug output of the Tolino reader, not an interface. If a
firmware update changes them, books still arrive but progress stops; the
settings page then says it has seen no page turn.

The system log also holds other apps' lines, among them the Thalia shop's
sign-in while the shop is open. The app asks logcat for the four tags above
only and keeps nothing but the book's path and the fraction.

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
   make android-install
   ```

   This builds the app in a container, installs it, grants it the log
   (`pm grant … android.permission.READ_LOGS`, which only adb can grant) and
   storage, and opens its settings. With an APK from elsewhere it is:

   ```bash
   adb install -r gotome-tolino.apk
   adb shell pm grant io.github.praetorianer777.gotome.tolino android.permission.READ_LOGS
   adb shell pm grant io.github.praetorianer777.gotome.tolino android.permission.WRITE_EXTERNAL_STORAGE
   ```

4. On the Tolino, in the app's settings:
   - **GOtome address**: how the Tolino reaches GOtome, e.g.
     `http://192.168.1.10:8080` (not `localhost`)
   - **User name** and **password** of your GOtome account. Only the session is
     kept, not the password; it lasts 30 days, then sign in again.
   - **Collection for this Tolino**: "Tolino" unless you want another. It is
     made in GOtome if you have none of that name.
5. **Sign in**. The app loads the catalogue, puts the "GOtome" book into the
   library and starts following the reader. The settings page should say that
   it may read the log.

USB debugging can be turned off again afterwards; the app keeps its
permissions.

## Use

- **Put a book on the Tolino from the web**: add it to your "Tolino"
  collection in GOtome. It appears on the Tolino when it is next online (at
  the latest within an hour, at once with "Sync" in the store).
- **From the Tolino**: open the book "GOtome" in your library, or the app from
  a launcher such as Simple Ink Launcher. Search, turn pages with ◀ ▶ or a
  swipe, tap a book, "Put on Tolino". The book is added to your "Tolino"
  collection too, so GOtome shows what is on the device.
- **Reading progress**: just read. Books you put on the Tolino through GOtome
  report their position; books copied by other means do not.

Deleting a book in the Tolino library deletes its file. The app does not bring
it back, even while it is still in the collection; "Put on Tolino" in the
store does.

## Development

```bash
make check-android   # unit tests and a debug build, in the container
make android-apk     # the APK, path printed at the end
make android-install # build, install over adb, grant the log
```

`app/src/test/resources/tolino-16.2.1.log` holds the reader's lines recorded on
a Shine 4 with firmware 16.2.1; a firmware that logs differently gets a
recording of its own next to it.
