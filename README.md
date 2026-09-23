# mymusidownloader

Personal daemon that turns a YouTube video into one audio file on disk. The phone starts the download over Tailscale. The file is not sent back over HTTP.

Open `http://pi:6874/` for a form and a table of tracks touched in the last month. You can edit title, artist, and genre there. Artist and genre stay empty until you set them. The same actions exist as HTTP calls.

Accepted input is a YouTube watch URL or an 11-character video id. Playlists, channels, and search URLs are rejected. There is no login.

## What it stores

Audio files go in `libraryPath`. JSON metadata files go in `metadataPath`. Those directories are separate.

The download starts under a temporary video-id name, then is renamed to `{YYYY-MM-DD}_{PascalTitle}.{ext}`. The date is the day the download started. If that name exists, the video id is appended.

MP3, AAC, FLAC, ALAC, and Opus are kept as downloaded. The YouTube thumbnail is center-cropped to a square and saved as album art, at most 1400 by 1400 pixels. When the album name is known, it is drawn on a dark bar at the bottom so it stays readable on a phone. Opus is kept as `.opus`. Most iOS Navidrome apps cannot play that. `POST /tracks/{videoId}/convert` starts an equivalent AAC conversion in the background and returns immediately with state `converting`. Opus 96 becomes AAC 128, Opus 128 becomes AAC 160, and Opus 160 becomes AAC 192. Same-bitrate conversion is rejected. Save is disabled while a track is converting. Any other codec is converted once to Opus, and only when the source bitrate is known. Tag edits do not encode the audio again.

## Build

Needs Go 1.22 or newer. No third-party Go modules. `yt-dlp` and `ffmpeg` are external programs, not part of the build.

From the repo root:

```sh
go test ./...
go build -o mymusidownloader ./cmd/mymusidownloader
```

That binary is for the machine you built it on. The Pi needs a Linux arm64 binary, built on the dev machine:

```sh
GOOS=linux GOARCH=arm64 go build -o mymusidownloader ./cmd/mymusidownloader
```

Do not build on the Pi. Copy that file to `/usr/local/bin/mymusidownloader`.

## Run locally

You can run it as your own user. The `musi` account is only for the Pi service. `ffmpeg` and `yt-dlp` must be on `PATH`, or set `ffmpegPath` and `ytDlpPath` in the config. Both directories must exist before start.

From the repo root:

```sh
mkdir -p download metadata
go run ./cmd/mymusidownloader -config config.sample.json
```

`config.sample.json` binds to `127.0.0.1:6874`, writes audio to `./download`, and writes JSON to `./metadata`. Open `http://127.0.0.1:6874/`. Leave `bind` empty only if you want the Tailscale address.

## Install on the Pi

The service runs as the user `musi` on a Raspberry Pi 5. `musi` has no home directory. It must be in the `media` group and able to write `/mnt/exthd/mymusi/download` and `/mnt/exthd/mymusi/data`. If either directory is missing or not writable, the process exits. It does not fall back to the SD card.

Install ffmpeg from apt. Do not install Debian's `yt-dlp` package. Put the current upstream `linux_aarch64` binary at `/usr/local/bin/yt-dlp` and make it executable.

Copy the arm64 binary from the build step to `/usr/local/bin/mymusidownloader`.

Create the system user without a home directory, then the config directory:

```sh
useradd --system --no-create-home --gid media --shell /usr/sbin/nologin musi
mkdir -p /etc/mymusidownloader
cp config.sample.json /etc/mymusidownloader/config.json
```

Edit that file before the first start. The sample binds to `127.0.0.1`. On the Pi, set `libraryPath` to `/mnt/exthd/mymusi/download`, set `metadataPath` to `/mnt/exthd/mymusi/data`, and remove `bind` so the daemon listens on the Tailscale address.

Install the system unit from `systemd/mymusidownloader.service` to `/etc/systemd/system/mymusidownloader.service`:

```sh
systemctl daemon-reload
systemctl enable --now mymusidownloader
```

## Config

The process reads one JSON file, passed with `-config`. There are no environment variables.

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
  "libraryPath": "/mnt/exthd/mymusi/download",
  "metadataPath": "/mnt/exthd/mymusi/data",
  "port": 6874,
  "maxConcurrent": 2,
  "ytDlpPath": "/usr/local/bin/yt-dlp",
  "ffmpegPath": "/usr/bin/ffmpeg"
}
```

## HTTP

`POST /trigger` with `{"url":"..."}` returns `videoId`, `state`, and `error` when rejected. The state is `rejected`, `stored`, `queued`, or `downloading`. The call returns when the download is accepted or started, not when the file is finished.

`POST /tracks/{videoId}` saves title, artist, album, and genre. It is rejected while the track is queued, downloading, or converting. `POST /tracks/{videoId}/cancel` stops a queued or running download. `POST /tracks/{videoId}/restart` restarts a failed or cancelled track. `POST /tracks/{videoId}/convert` accepts only equivalent AAC conversion, sets state `converting`, and returns without waiting for ffmpeg. `POST /tracks/{videoId}/delete` removes the audio file, the JSON record, and the cover. It is rejected while the track is queued, downloading, or converting. There is no file download and no status API.
