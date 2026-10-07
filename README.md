# mymusidownloader

A daemon that saves the audio of one YouTube video to disk. The page starts the work. The audio file is not sent back over HTTP.

Open `http://music.example:6874/`. There is no login.

## Tools

`ffmpeg` is already installed with the distro package manager. `yt-dlp` is not. The distro package of `yt-dlp` is usually too old to finish a download.

```sh
curl -fL -o /tmp/yt-dlp https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux_aarch64
sudo install -m 755 /tmp/yt-dlp /usr/local/bin/yt-dlp
```

On a non-ARM machine, download `yt-dlp_linux` instead of `yt-dlp_linux_aarch64`.

## Run locally

Both directories must exist before start. The process exits if either one is missing or not writable.

```sh
mkdir -p download metadata
go run ./cmd/mymusidownloader --config packaging/config.sample.json
```

Open `http://127.0.0.1:6874/`. That sample config binds to localhost, so another machine cannot open it.

## Install as a service

A first run installs the binary, the unit file, and the sample config. It does not start the service. A later run says it is an update, overwrites the binary, and restarts the service if it is already running.

```sh
curl -fsSL https://raw.githubusercontent.com/Tibart/mymusidownloader/main/install.sh | sudo sh
```

From a checkout:

```sh
sudo sh install.sh packaging/mymusidownloader.sample.service packaging/config.sample.json
```

## After install

The names below are examples. Use your own user, group, and disk.

1. Create the service user. It has no home directory because the unit file sets the account, and the program does not read a home directory.

```sh
sudo groupadd --system media
sudo useradd --system --no-create-home --gid media --shell /usr/sbin/nologin musi
```

2. Create the two data directories and make them writable by that user. The process exits if it cannot create a test file there. It does not write somewhere else. `/srv/music` is for the audio files, because another program reads them too. `/var/lib/mymusidownloader` is for the JSON records and covers, because only this service needs them.

```sh
sudo mkdir -p /srv/music /var/lib/mymusidownloader
sudo chown musi:media /srv/music /var/lib/mymusidownloader
sudo chmod 2775 /srv/music /var/lib/mymusidownloader
```

Group `media` is only for write access. On exFAT, `chown` does not work. Set the mount `gid` to the group that should write, with `dmask=0002`.

3. Edit `/etc/mymusidownloader/config.json`. Set `libraryPath` to `/srv/music` and `metadataPath` to `/var/lib/mymusidownloader`. Remove `bind` if other machines on the private network should open the page. A set `bind` is used as written. An empty `bind` uses the private-network address, or `127.0.0.1` if that address is absent.

4. Edit `User=` and `Group=` in `/etc/systemd/system/mymusidownloader.service` if they are not the account from step 1. The JSON file cannot set the user.

5. Start it. `enable` only prints the symlink it created. A successful start prints nothing. Check with `systemctl status`.

```sh
sudo systemctl enable --now mymusidownloader
systemctl status mymusidownloader
```

Open `http://music.example:6874/`. Type `http://`. There is no HTTPS listener.

If status is `203/EXEC`, the binary is not executable or is the wrong CPU. `file /usr/local/bin/mymusidownloader` must say `ARM aarch64` on an ARM machine. Then `sudo chmod 755` that file.

## Config files

`packaging/config.sample.json` is the template. The installer copies it to `/etc/mymusidownloader/config.json` only when that file does not exist yet.

| Field | Sample value | Meaning |
| --- | --- | --- |
| `libraryPath` | `./download` | Audio files. Must already exist and be writable. |
| `metadataPath` | `./metadata` | JSON records and cover images. Separate from the audio directory. |
| `port` | `6874` | HTTP port. |
| `maxConcurrent` | `2` | Downloads running at once. Further requests wait. |
| `bind` | `127.0.0.1` | Listen address. Remove it to listen on the private-network address. |
| `ytDlpPath` | `/usr/local/bin/yt-dlp` | `yt-dlp` binary. |
| `ffmpegPath` | `/usr/bin/ffmpeg` | `ffmpeg` binary. |

The sample values are for a local run. For the service, set `libraryPath` to `/srv/music` and `metadataPath` to `/var/lib/mymusidownloader`.

`packaging/mymusidownloader.sample.service` is the systemd template. The installer copies it to `/etc/systemd/system/mymusidownloader.service` and replaces that copy on every run.

| Setting | Sample value | Meaning |
| --- | --- | --- |
| `User` | `musi` | Account the process runs as. |
| `Group` | `media` | Group used for write access to the data directories. |
| `ExecStart` | `/usr/local/bin/mymusidownloader --config /etc/mymusidownloader/config.json` | Binary and the JSON config it reads. `-c` is the short form. |
| `Restart` | `on-failure` | systemd starts it again after a crash. |

## What a download stores

The file is stored as `{Artist}/{Album}/{YYYY-MM-DD}_{PascalTitle}.{ext}`. The artist folder is `Various Artists` when that box is checked. The date is the day the download started. A taken name gets the video id appended. The YouTube URL is stored in the JSON record and in the file tags. The cover links to that URL.

An empty album name becomes the title. Check Various Artists to write the album artist as `Various Artists`. Players group tracks only when both the album name and the album artist match.

The cover is a square crop of the YouTube thumbnail, at most 1024 by 1024. The artist name is drawn on a dark bar. Opus stays `.opus`. Most iOS players cannot play that. Convert it to AAC when an iPhone app must play it. Opus 96 becomes AAC 128, Opus 128 becomes AAC 160, and Opus 160 becomes AAC 192.

## HTTP

Examples use `http://127.0.0.1:6874`. Replace the host when the daemon is elsewhere.

`GET /` is the page.

`GET /tracks/abc123def45/artwork` is the cover JPEG. Missing art is 404.

`POST /trigger` accepts a watch URL or an 11-character video id. It returns when the download is accepted, not when the file is finished. `rejected` means the URL was not a single video. `stored` means that video is already on disk. `queued` means it is waiting for a slot. `downloading` means it has started.

```sh
curl -sS -X POST http://127.0.0.1:6874/trigger \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://www.youtube.com/watch?v=abc123def45"}'
```

`POST /tracks/abc123def45` saves title, artist, album, genre, and the Various Artists flag. A non-empty new genre is added to the lookup. This is rejected while the track is queued, downloading, or converting.

```sh
curl -sS -X POST http://127.0.0.1:6874/tracks/abc123def45 \
  -H 'Content-Type: application/json' \
  -d '{"title":"Example Song","artist":"Example Artist","album":"Example Album","genre":"Pop","variousArtists":true}'
```

`POST /tracks/abc123def45/cancel` stops a queued or running download. The row stays, with state `cancelled`. A finished track cannot be cancelled.

```sh
curl -sS -X POST http://127.0.0.1:6874/tracks/abc123def45/cancel
```

`POST /tracks/abc123def45/restart` starts the same track again. Only a failed or cancelled track can be restarted.

```sh
curl -sS -X POST http://127.0.0.1:6874/tracks/abc123def45/restart
```

`POST /tracks/abc123def45/convert` starts an equivalent AAC conversion and returns immediately with state `converting`. Same-bitrate conversion is rejected. Save is rejected until it finishes.

```sh
curl -sS -X POST http://127.0.0.1:6874/tracks/abc123def45/convert \
  -H 'Content-Type: application/json' \
  -d '{"equivalent":true}'
```

`POST /tracks/abc123def45/delete` removes the audio file, the JSON record, and the cover. It is rejected while the track is queued, downloading, or converting.

```sh
curl -sS -X POST http://127.0.0.1:6874/tracks/abc123def45/delete
```
