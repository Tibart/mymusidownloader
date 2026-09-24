# mymusidownloader

Personal daemon that turns a YouTube video into one audio file on disk. The phone starts the download over Tailscale. The file is not sent back over HTTP.

Open `http://music.example:6874/` for a form and a table of tracks touched in the last month. You can edit title, artist, and genre there. Artist and genre stay empty until you set them. The same actions exist as HTTP calls.

Accepted input is a YouTube watch URL or an 11-character video id. Playlists, channels, and search URLs are rejected. There is no login.

## What it stores

Audio files go in `libraryPath`. JSON metadata files go in `metadataPath`. Those directories are separate.

The download starts under a temporary video-id name, then is renamed to `{YYYY-MM-DD}_{PascalTitle}.{ext}`. The date is the day the download started. If that name exists, the video id is appended.

MP3, AAC, FLAC, ALAC, and Opus are kept as downloaded. The YouTube thumbnail is center-cropped to a square and saved as album art, at most 1024 by 1024 pixels. When the album name is empty, it becomes the title. The cover text is the artist name. Genre is a lookup of names already used, stored in `genres.json`, and a new name can be typed. Check Various Artists to write album artist as Various Artists. Players then group tracks that share that album artist and the same album name. The album name is drawn on a dark bar at the bottom of the cover. Opus is kept as `.opus`. Most iOS Navidrome apps cannot play that. `POST /tracks/{videoId}/convert` starts an equivalent AAC conversion in the background and returns immediately with state `converting`. Opus 96 becomes AAC 128, Opus 128 becomes AAC 160, and Opus 160 becomes AAC 192. Same-bitrate conversion is rejected. Save is disabled while a track is converting. Any other codec is converted once to Opus, and only when the source bitrate is known. Tag edits do not encode the audio again.

## Build

Needs Go 1.22 or newer. No third-party Go modules. `yt-dlp` and `ffmpeg` are external programs, not part of the build.

From the repo root:

```sh
make test
make build
make build-arm64
```

`make build` writes the binary for this machine to `bin/mymusidownloader`. `make build-arm64` writes the Pi binary to `bin/linux-arm64/mymusidownloader`. `bin/` is gitignored. Do not build on the Pi. Copy `bin/linux-arm64/mymusidownloader` to `/usr/local/bin/mymusidownloader`.

## Run locally

You can run it as your own user. The `app` account in the examples is only for the service. `ffmpeg` and `yt-dlp` must be on `PATH`, or set `ffmpegPath` and `ytDlpPath` in the config. Both directories must exist before start.

From the repo root:

```sh
mkdir -p download metadata
go run ./cmd/mymusidownloader -config config.sample.json
```

`config.sample.json` binds to `127.0.0.1:6874`, writes audio to `./download`, and writes JSON to `./metadata`. Open `http://127.0.0.1:6874/`. Leave `bind` empty only if you want the Tailscale address.

## Install on the Pi

The examples use user `app`, group `media`, host `music.example`, and data on `/mnt/music`. None of these names are required. systemd sets the user in the unit file, not in `config.json`. The example user has no home directory. If a data directory is missing or not writable, the process exits. It does not fall back to another disk.

1. On the dev machine, build the Pi binary: `make build-arm64`.
2. On the Pi, install ffmpeg: `sudo apt install ffmpeg`.
3. Install the current upstream `yt-dlp`, not the Debian package:

```sh
curl -fL -o /tmp/yt-dlp https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux_aarch64
sudo install -m 755 /tmp/yt-dlp /usr/local/bin/yt-dlp
```

4. Copy the daemon and make it executable:

```sh
scp bin/linux-arm64/mymusidownloader admin@music.example:/tmp/mymusidownloader
ssh admin@music.example 'sudo install -m 755 /tmp/mymusidownloader /usr/local/bin/mymusidownloader'
```

5. Create the user, group, and directories. Group `media` is only for write access. The service user is a member of it, and the data directories are group-owned with mode `2775`, so that user can create files without owning the disk as root. Other accounts in `media` can write there too. If the disk is exFAT, skip `chown` and set the mount `gid` to this group instead.

```sh
sudo groupadd --system media
sudo useradd --system --no-create-home --gid media --shell /usr/sbin/nologin app
sudo mkdir -p /mnt/music/download /mnt/music/data /etc/mymusidownloader
sudo chown app:media /mnt/music/download /mnt/music/data
sudo chmod 2775 /mnt/music/download /mnt/music/data
```

6. Write `/etc/mymusidownloader/config.json` with the example paths below, or your own. Do not set `bind` if the page should be reached only on the private network.
7. Copy `systemd/mymusidownloader.service` to `/etc/systemd/system/mymusidownloader.service`.
8. Start it:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now mymusidownloader
systemctl status mymusidownloader
```

`enable` only prints the symlink it created. A successful start prints nothing. Open `http://music.example:6874/`. Use `http://`, not `https://`.

## Troubleshooting

| What you see | What it means | What to do |
| --- | --- | --- |
| `status=203/EXEC` | The binary cannot be executed. | `file /usr/local/bin/mymusidownloader` must say `ARM aarch64`. Then `sudo chmod 755` that file. `scp` often drops the execute bit. |
| `library path is not writable` | The service user cannot create a file there. The directory can still exist. | `sudo -u app touch /mnt/music/download/.write-test`. On exFAT, `chown` fails. Remount with `gid` set to the `media` group and `dmask=0002`. |
| `chown: Operation not permitted` | The disk is exFAT or NTFS. | Do not use `chown`. Set `uid` and `gid` in the mount options. |
| Page works on the phone, not in a desktop browser | The desktop is not on the same private network as the daemon. | Join that network, then open `http://music.example:6874/`. A home-network name is not used unless `bind` includes that address. |
| Browser shows nothing on the Tailscale name | The browser switched to HTTPS. | Type `http://` and port `6874`. There is no HTTPS listener. |
| `enable --now` only prints a symlink | That is normal. | `systemctl status mymusidownloader` and `journalctl -u mymusidownloader -n 30 --no-pager`. |

## Config

The process reads one JSON file, passed with `-config`. There are no environment variables. The JSON file does not set the user. systemd does that in the unit file. The example unit uses `User=app` and `Group=media`. Change those to the account you created.

| Field | Default | Meaning |
| --- | --- | --- |
| `libraryPath` | `./download` | Directory for audio files only. Must already exist and be writable. |
| `metadataPath` | `./metadata` | Directory for the JSON record of each track. Must already exist and be writable. |
| `port` | `6874` | HTTP port. |
| `maxConcurrent` | `2` | Downloads running at once. Further requests are queued. |
| `bind` | Tailscale IPv4, else `127.0.0.1` | Listen address. An empty value uses the default. A set value is used as written. |
| `ytDlpPath` | `/usr/local/bin/yt-dlp` | `yt-dlp` binary. |
| `ffmpegPath` | `/usr/bin/ffmpeg` | `ffmpeg` binary. |

Pi config:

```json
{
  "libraryPath": "/mnt/music/download",
  "metadataPath": "/mnt/music/data",
  "port": 6874,
  "maxConcurrent": 2,
  "ytDlpPath": "/usr/local/bin/yt-dlp",
  "ffmpegPath": "/usr/bin/ffmpeg"
}
```

## HTTP

`POST /trigger` with `{"url":"..."}` returns `videoId`, `state`, and `error` when rejected. The state is `rejected`, `stored`, `queued`, or `downloading`. The call returns when the download is accepted or started, not when the file is finished.

`POST /tracks/{videoId}` saves title, artist, album, and genre. It is rejected while the track is queued, downloading, or converting. `POST /tracks/{videoId}/cancel` stops a queued or running download. `POST /tracks/{videoId}/restart` restarts a failed or cancelled track. `POST /tracks/{videoId}/convert` accepts only equivalent AAC conversion, sets state `converting`, and returns without waiting for ffmpeg. `POST /tracks/{videoId}/delete` removes the audio file, the JSON record, and the cover. It is rejected while the track is queued, downloading, or converting. There is no file download and no status API.

Examples use a local daemon. On another machine, replace the host with `music.example`.

```sh
curl -sS -X POST http://127.0.0.1:6874/trigger \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://www.youtube.com/watch?v=abc123def45"}'

curl -sS -X POST http://127.0.0.1:6874/tracks/abc123def45 \
  -H 'Content-Type: application/json' \
  -d '{"title":"Example Song","artist":"Example Artist","album":"","genre":"Pop"}'

curl -sS -X POST http://127.0.0.1:6874/tracks/abc123def45/cancel
curl -sS -X POST http://127.0.0.1:6874/tracks/abc123def45/restart
curl -sS -X POST http://127.0.0.1:6874/tracks/abc123def45/convert \
  -H 'Content-Type: application/json' \
  -d '{"equivalent":true}'
curl -sS -X POST http://127.0.0.1:6874/tracks/abc123def45/delete
```
