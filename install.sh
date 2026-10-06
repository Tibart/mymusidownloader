#!/bin/sh
# Install the systemd unit and config file.
# Local:  sudo ./install.sh [service-file] [config-file]
# Remote: curl -fsSL https://raw.githubusercontent.com/Tibart/mymusidownloader/main/install.sh | sudo sh
set -eu

repo="${MYMUSIDOWNLOADER_REPO:-https://raw.githubusercontent.com/Tibart/mymusidownloader/main}"
unit_dest="/etc/systemd/system/mymusidownloader.service"
config_dest="/etc/mymusidownloader/config.json"

if [ "$(id -u)" -ne 0 ]; then
  echo "Run as root: sudo sh install.sh" >&2
  exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

service_file="${1:-}"
config_file="${2:-}"

if [ -z "$service_file" ]; then
  service_file="$tmp/mymusidownloader.service"
  curl -fsSL "$repo/packaging/mymusidownloader.sample.service" -o "$service_file"
fi
if [ -z "$config_file" ]; then
  config_file="$tmp/config.sample.json"
  curl -fsSL "$repo/packaging/config.sample.json" -o "$config_file"
fi

if [ ! -f "$service_file" ] || [ ! -f "$config_file" ]; then
  echo "Need a service file and a config file." >&2
  exit 1
fi

install -d -m 755 /etc/mymusidownloader
install -m 644 "$service_file" "$unit_dest"
if [ -f "$config_dest" ]; then
  echo "Left existing $config_dest in place."
else
  install -m 644 "$config_file" "$config_dest"
  echo "Installed $config_dest"
fi
systemctl daemon-reload

cat <<'EOF'

Installed the service file. The service was not started.

Edit /etc/mymusidownloader/config.json before the first start:
  libraryPath   directory for audio files, must already exist and be writable
  metadataPath  directory for JSON records, must already exist and be writable
  port          default 6874
  bind          remove this line to listen on the private-network address
  ytDlpPath     /usr/local/bin/yt-dlp
  ffmpegPath    /usr/bin/ffmpeg

The user is set in the unit file, not in the JSON file. The example unit uses User=app and Group=media. Change those to the account that can write the two directories.

Then:
  sudo systemctl enable --now mymusidownloader
  systemctl status mymusidownloader

Open http://<host>:6874/ with http, not https.
EOF
