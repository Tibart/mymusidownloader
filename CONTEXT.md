# My Music Downloader

A personal daemon that turns a YouTube video into one stored audio Track. The phone starts the work and later edits what the player shows. The file never comes back through HTTP.

## Language

**Track**:
One YouTube video kept as one audio file plus its JSON sidecar.
_Avoid_: Song, download, job, file

**Audio file**:
The stored sound, kept in the source codec when that codec is allowed. Allowed: MP3, AAC, FLAC, ALAC, Opus.
_Avoid_: MP3, download

**Check**:
The cheap gate before a Track is accepted: a YouTube watch URL or an 11-character video id, not a playlist, channel, or search URL, and not a video id already stored.
_Avoid_: Validation, probe, metadata lookup

**Trigger**:
A request that asks the daemon to start work on a URL. It has two doors: the Recent page form and the HTTP endpoint.
_Avoid_: Submit, job, request

**Start result**:
What a Trigger is told immediately: `rejected`, `stored`, `queued`, or `downloading`.
_Avoid_: Status, response code, job state

**Cancel**:
Stop a queued or downloading Track. The row remains, with state `cancelled`. Partial audio is removed. A done Track cannot be cancelled.
_Avoid_: Delete, abort

**Recent page**:
The HTML page that lists Tracks from the last month and can edit a stored Track.
_Avoid_: Status API, dashboard, library

**Title**:
The video name, used as the Track's initial name. It can be edited later.
_Avoid_: Filename, song name

**Artist**:
Unknown until edited on the Recent page.
_Avoid_: Uploader, channel

**Genre**:
Unknown until edited on the Recent page.
_Avoid_: Category, tag

**File name**:
`{YYYY-MM-DD}_{PascalTitle}`, using the download date. If that name is taken, the video id is appended.
_Avoid_: Video id, slug

## Relationships

- A **Trigger** produces one **Start result**
- A **Check** failure produces `rejected` and no **Track**
- A video id with an MP3 already in the **Library** produces `stored` and no second **Track**
- A failed **Track** restarted by a **Trigger** is the same **Track**, not a new one
- A **Track** that is queued or downloading returns that same **Start result** and does not start a second copy
- **Cancel** kills a running download and leaves the **Track** as `cancelled`
- **Restart** is allowed only when a **Track** is failed or `cancelled`, and it is the same **Track**
- A **Track** has one **Audio file**, one **Title**, one **Artist**, and one **Genre**
- A **Track** has one **File name**
- The **Recent page** lists every **Track** touched in the last month, including queued, downloading, done, and failed, and is the only place **Artist** and **Genre** are set
- While any visible **Track** is queued or downloading, the **Recent page** reloads itself every 5 seconds
- A row shows download date, **Title**, **Artist**, **Genre**, state, and **File name**. There is no delete.

## Example dialogue

> **Dev:** "The phone sent a URL. Do we wait until the MP3 is finished?"
> **Domain expert:** "No. A **Trigger** returns a **Start result** once the work is accepted or has started. The phone never downloads the file."

> **Dev:** "The channel name is right there. Use it as **Artist**?"
> **Domain expert:** "No. **Artist** and **Genre** stay unknown until someone edits them on the **Recent page**. **Title** starts as the video name."

## Flagged ambiguities

- "Status API" was proposed, then dropped. Progress after the **Start result** is visible only on the **Recent page**, including rows that do not have an MP3 yet.
- "Date" in the **File name** is the download date, not the video's publish date.
- The project path is `/mnt/c/Repos/mymusidownloader`, not a folder inside the tax vault.
- The daemon is a systemd user service on a Raspberry Pi 5, Tailscale name `pi`, running as the normal user `musi`. That user is created for this daemon and added to the group `media`, which can write `/mnt/exthd/music`. Settings live in `/home/musi/.config/mymusidownloader/config.json`.
- `/mnt/c/Repos/mymusidownloader` is the editing repo. It is not the path on the Pi.
- `yt-dlp` on the Pi is the current upstream aarch64 binary, not the distro package. `ffmpeg` may come from apt.
- A new build is cross-compiled to `linux/arm64` and copied to the Pi. The Pi does not build it.
- On the Pi the **Library** setting points at `/mnt/exthd/music`. If that path is missing or not writable, the daemon does not start and does not write to the SD card.
- There is no login yet. Who may send a **Trigger** is a network decision, not an account. Default listen is the Tailscale address, or `127.0.0.1` if Tailscale is absent. Not every interface.
- Private, age-gated, and login-only videos are out of scope. A fetch that needs a YouTube login fails the **Track**. No cookies and no Google account.
- "MP3" was the original store format. Resolved: keep the source codec when it is an allowed **Audio file**. If the codec is not allowed, convert once to Opus at a bitrate no higher than the source. Tag edits do not encode again.
