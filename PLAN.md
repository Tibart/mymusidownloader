# Plan

Personal Go daemon. Phone on Tailscale starts a YouTube audio download. File stays on disk. Page shows last month and edits tags.

## Run

- Code: `/mnt/c/Repos/mymusidownloader`
- Pi 5, user `musi`, no home directory, group `media`, systemd system service
- Config: `/etc/mymusidownloader/config.json`
- Library on Pi: `/mnt/exthd/mymusi/download`. Metadata: `/mnt/exthd/mymusi/data`. Either missing or not writable: exit, no SD fallback
- Dev library default: `download/` in the repo
- Listen: Tailscale address, else `127.0.0.1`. Port setting, default `6874`. No login
- Binary: cross-compile `linux/arm64` here, copy to `/usr/local/bin/mymusidownloader`
- `yt-dlp`: upstream aarch64 binary at `/usr/local/bin/yt-dlp`. `ffmpeg` from apt

## Trigger

- Doors: `GET /` form and `POST /trigger` with `{"url":"..."}`
- Check: watch URL or 11-char video id. Reject playlist, channel, search
- Already has an audio file: `stored`
- Failed or `cancelled`: restart same Track
- Queued or downloading: return that state, no second copy
- Slot free: `downloading`. Else `queued`. Max concurrent setting, default 2
- Response does not wait for the file
- No cookies, no Google login. Those failures stay `failed`

## Files

- Temp name is the video id. Rename to `{YYYY-MM-DD}_{PascalTitle}.{ext}` when the title is known
- Date is the download date. Name taken: append video id
- Keep source codec if MP3, AAC, FLAC, ALAC, or Opus. Else one Opus convert, bitrate not above source
- JSON sidecar is the record. Tag edit writes native tags, no second encode
- Title starts as the video name. Artist and genre stay empty until edited

## Page

- Last month: queued, downloading, done, failed, cancelled
- Columns: date, title, artist, genre, state, file name
- Edit those three fields: `POST /tracks/{videoId}`
- Cancel queued or downloading: `POST /tracks/{videoId}/cancel`. Kill process, delete partial audio, row stays `cancelled`. Done cannot be cancelled
- Restart failed or cancelled: `POST /tracks/{videoId}/restart`
- No delete, no file download, no status API
- Reload every 5s while any visible row is queued or downloading

## Blocking

- Pi username spelling `musi` not confirmed on the machine
- Tailscale hostname is `pi`
- Group `media` and the directories `/mnt/exthd/mymusi/download` and `/mnt/exthd/mymusi/data` are created outside this repo
