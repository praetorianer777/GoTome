# Tolinos on the Kobo-based firmware

Since 2024 the newer Tolinos (Shine 5, Shine Color, Vision Color) run the
same firmware as Kobo's readers: Linux with Kobo's reading software in
Tolino colours, version 5.x. GOtome for Tolino (`clients/android`) needs the
Android firmware of the older Tolinos (Shine 4 and earlier, firmware 16.x)
and does not run on them.

The firmware runs in one of two modes:

| | Tolino mode (as sold) | Kobo mode |
|---|---|---|
| Shop | Thalia or the other Tolino booksellers | Kobo |
| Sync | Tolino cloud | Kobo's sync, or a server set in the config |
| Firmware updates | Over Wi-Fi | Offered over Wi-Fi again once developer mode is off |

In Kobo mode the reader syncs with whatever server
`api_endpoint` names in its config, as a Kobo does. That is how GOtome
reaches it (#230). In Tolino mode the reader ignores that line.

Nothing here is supported by Tolino or Kobo. The steps rely on the
firmware's own update mechanism and on the community findings in
[KoboTolinoFindings](https://github.com/notmarek/KoboTolinoFindings).

## What was checked

On a Tolino Shine 5, October 2026:

| Firmware | Mode | Result |
|---|---|---|
| 5.13.241423 | Tolino | `api_endpoint` added under `[OneStoreServices]` is kept in the config but never called; sync goes to the Tolino cloud |
| 5.18.264769 | Tolino, developer mode on | The developer-mode package works: tapping search opens the developer menu |
| 5.18.264769 | Kobo | A sync asks the `api_endpoint` server for `GET /v1/initialization`, `GET /v1/user/profile`, `GET /v1/user/loyalty/benefits`, `GET /v1/deals` and `POST /v1/analytics/gettests` |
| 5.18.264769 | Kobo, developer mode off again | Still Kobo mode, still syncs with the `api_endpoint` server; firmware 6.0 is offered over Wi-Fi |

Things learnt on the way:

- In Tolino mode, **Sync** at the top right also installs firmware updates
  without asking. On 5.13 it updated twice in a row, to 5.15.253009 and
  then 5.18.264769. The config survived both updates.
- Switching to Kobo mode **wipes the reader**: books, settings, the
  Tolino cloud sign-in.
- Kobo mode needs an activation with a Kobo account (a free one is
  enough). The way around it on Kobo 4.x, a row in the `user` table of
  `KoboReader.sqlite`, does not work here, because the reader has already
  registered itself with Kobo by then.
- Firmware updates are not asked of the `api_endpoint` server, so GOtome
  cannot hold them back.

## Before you start

- **Back up the reader.** Connect it over USB and copy everything on it
  except `.kobo/swapfile` (512 MB, made again by the firmware). The books
  are the folders next to `.kobo`; `.adobe-digital-editions` holds the
  Adobe authorisation for books with DRM.
- **Leave firmware updates alone** in Tolino mode while you prepare: tap
  **Sync** only when you mean to update.
- Books from Thalia with DRM stay readable after the switch if their files
  and `.adobe-digital-editions` are copied back. The Thalia shop, the
  Tolino cloud and the Onleihe are not reachable in Kobo mode.

## 1. Turn on developer mode

1. Get `EnableDevmode/update.tar` from
   [KoboTolinoFindings](https://github.com/notmarek/KoboTolinoFindings).
   It is stored with git-lfs: a plain download or `git clone` without
   `git lfs pull` gives a 134-byte pointer file, which does nothing. The
   real file is 10240 bytes with SHA-256
   `a6a8044c69cb682624562f7090c5cb2c500609fbecac6694d802cab0601c75be`.
2. Copy it to `.kobo/update.tar` on the reader and eject it.

The reader runs the update and reports it as failed. Its script changes
one word in the firmware's branch file (`/etc/rootfs-branch` or
`/usr/local/Kobo/branch`), from `release/` to `dev/`, and fails on
purpose so the updater does not reboot into recovery. Nothing is flashed.

Afterwards tapping the search icon opens the developer menu.

## 2. Switch to Kobo mode

1. In the developer menu, switch from Tolino to Kobo. The reader wipes
   itself and restarts.
2. When it asks, turn down the **optional accessibility features**. They
   would install a firmware update.
3. Activate it with a Kobo account: the reader shows a code for
   `kobo.com/activate`.
4. Copy your books back from the backup.

`.kobo/affiliate.conf` now says `affiliate=Kobo`, and the config has a
`[OneStoreServices]` section with `api_endpoint=https://storeapi.kobo.com`.

## 3. Turn developer mode off again

With developer mode on, the reader shows debug text and search opens the
developer menu instead of searching. Turning it off is the same package the
other way round: a `driver.sh` that changes `dev/` back to `release/`.

```sh
#!/bin/sh
set -e
case "$2" in
    stage1)
        if [ -f /etc/rootfs-branch ]; then
            sed -i 's#dev/#release/#' /etc/rootfs-branch
        elif [ -f /usr/local/Kobo/branch ]; then
            sed -i 's#dev/#release/#' /usr/local/Kobo/branch
        fi
        # Reported as failed so the updater does not reboot into recovery.
        exit 1
        ;;
    stage2)
        set_boot_part root
        ;;
esac
sync
```

Pack it as `update.tar` with `driver.sh` at its top
(`tar --owner=0 --group=0 -cf update.tar driver.sh`, the script executable),
copy it to `.kobo/update.tar` and eject. The reader stays in Kobo mode.
The developer menu's log setting, `Log="*.debug=true\nqt.*.debug=false"`
under `[DeveloperSettings]` in the config, can be deleted along with it.

To go back to Tolino mode later, turn developer mode on again and switch in
its menu. That wipes the reader once more.

## 4. Point the reader at GOtome

Connect the reader over USB, open `.kobo/Kobo/Kobo eReader.conf` and
change the line under `[OneStoreServices]`:

```ini
api_endpoint=https://storeapi.kobo.com
```

to the line GOtome shows for the device. In GOtome, on your profile under
**E-readers**, **Add a device** named after the reader; with its token it
shows the line for a Kobo reader:

```ini
api_endpoint=https://gotome.example.org/api/v1/kobo/abcd-efgh-jkmn-pqrs-tuvw
```

The token is in the address, as the reader sends no credentials of its own
to the store it is given; treat the line like a password. The address must
be one the reader reaches: on your own network, the server's IP and port,
not `localhost`. Eject and tap **Sync**.

From then on a sync brings the books sent to the device in GOtome (**Send
to an e-reader** on a book, or from a list), with their covers and details,
and the reading place both ways: reading on the reader moves the book's
place in GOtome, and reading further elsewhere moves it on the reader at
its next sync. A book taken back before the reader has it leaves the
reader; one archived on the reader leaves the device in GOtome. Revoking
the device on the profile cuts the reader off.

The store's pages, its search and purchases stay Kobo's, as the reader's
Kobo account has them. Collections, notes and highlights are not synced
yet.

Watch the requests before GOtome answers them: a plain HTTP server that
logs, such as `python3 -m http.server <port>` run in an empty directory,
shows each path the reader asks for. The reader must reach it, so check
the PC's firewall.

## Firmware updates

With developer mode off, the reader offers Kobo's updates over Wi-Fi again.
Firmware 6.0 was offered on 5.18.264769 and not installed: it was unknown
whether it still honours `api_endpoint`, and whether the way back still
works from it. Others report boot loops after over-the-air updates on
converted Tolinos. Blocking `ereaderfiles.kobo.com`, where the firmware
comes from, in the router or a DNS filter keeps the offers away without
touching sync.

The way back to Kobo's 4.x firmware, should a 5.x or 6.x update go wrong,
is the downgrade package in KoboTolinoFindings, applied in Kobo mode. The
recovery partition is the safety net: Kobo's factory reset with the power
button restores the firmware from it. Steps that write to the recovery
partition remove that net.
