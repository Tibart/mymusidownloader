#!/bin/sh
# Install or update the daemon, unit file, and config.
# Local:  sudo ./install.sh [service-file] [config-file] [binary]
# Remote: curl -fsSL https://raw.githubusercontent.com/Tibart/mymusidownloader/main/install.sh | sudo sh
set -eu

repo="${MYMUSIDOWNLOADER_REPO:-https://raw.githubusercontent.com/Tibart/mymusidownloader/main}"
release="${MYMUSIDOWNLOADER_RELEASE:-https://github.com/Tibart/mymusidownloader/releases/latest/download/mymusidownloader}"
unit_dest="/etc/systemd/system/mymusidownloader.service"
config_dest="/etc/mymusidownloader/config.json"
bin_dest="/usr/local/bin/mymusidownloader"

if [ "$(id -u)" -ne 0 ]; then
  echo "Run as root: sudo sh install.sh" >&2
  exit 1
fi

case "$(uname -m)" in
  aarch64|arm64) ;;
  *)
    echo "The release binary is linux/arm64. This machine is $(uname -m)." >&2
    exit 1
    ;;
esac

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

service_file="${1:-}"
config_file="${2:-}"
binary_file="${3:-}"

if [ -z "$service_file" ]; then
  service_file="$tmp/mymusidownloader.service"
  curl -fsSL "$repo/packaging/mymusidownloader.sample.service" -o "$service_file"
fi
if [ -z "$config_file" ]; then
  config_file="$tmp/config.sample.json"
  curl -fsSL "$repo/packaging/config.sample.json" -o "$config_file"
fi
release_number=""
if [ -z "$binary_file" ]; then
  binary_file="$tmp/mymusidownloader"
  release_url="$(curl -fsSL -o "$binary_file" -w '%{url_effective}' "$release")"
  release_number="$(printf '%s
' "$release_url" | sed -n 's|.*/download/v\([^/]*\)/.*|\1|p')"
fi

if [ ! -f "$service_file" ] || [ ! -f "$config_file" ] || [ ! -f "$binary_file" ]; then
  echo "Need a service file, a config file, and a binary." >&2
  exit 1
fi

updating=0
previous_version=""
if [ -e "$bin_dest" ] || [ -e "$unit_dest" ] || [ -e "$config_dest" ]; then
  updating=1
fi
if [ -x "$bin_dest" ]; then
  previous_version="$("$bin_dest" --version 2>/dev/null || true)"
fi

install -d -m 755 /etc/mymusidownloader /usr/local/bin
install -m 755 "$binary_file" "$bin_dest"
installed_version="$(printf '%s' "$("$bin_dest" --version 2>/dev/null || true)" | tr -d '\r\n')"
installed_number="${installed_version#mymusidownloader }"
previous_number="${previous_version#mymusidownloader }"
if [ -z "$installed_number" ] || [ "$installed_number" = "$installed_version" ]; then
  installed_number="${release_number:-unknown}"
fi
if [ -z "$previous_number" ] || [ "$previous_number" = "$previous_version" ]; then
  previous_number="unknown"
fi
if [ "$updating" -eq 1 ]; then
  if [ "$previous_number" = "unknown" ] || [ "$previous_number" = "$installed_number" ]; then
    echo "Updated mymusidownloader to $installed_number."
  else
    echo "Updated mymusidownloader from version $previous_number to $installed_number."
  fi
else
  echo "Installed mymusidownloader version $installed_number."
fi
install -m 644 "$service_file" "$unit_dest"
if [ ! -f "$config_dest" ]; then
  install -m 644 "$config_file" "$config_dest"
  echo "Installed $config_dest"
fi
systemctl daemon-reload

if [ "$updating" -eq 1 ] && systemctl is-active --quiet mymusidownloader; then
  systemctl restart mymusidownloader
  echo "Restarted mymusidownloader."
  exit 0
fi

if [ "$updating" -eq 1 ]; then
  echo "Binary replaced. Start it with: sudo systemctl restart mymusidownloader"
  exit 0
fi

cat <<'EOF'

Installed the binary and the service file. The service was not started.

Edit /etc/mymusidownloader/config.json before the first start:
  libraryPath   directory for audio files, must already exist and be writable
  metadataPath  directory for JSON records, must already exist and be writable
  port          default 6874
  bind          remove this line to listen on the private-network address
  ytDlpPath     /usr/local/bin/yt-dlp
  ffmpegPath    /usr/bin/ffmpeg

The user is set in the unit file, not in the JSON file. The example unit uses User=musi and Group=media. Change those to the account that can write the two directories.

Then:
  sudo systemctl enable --now mymusidownloader
  systemctl status mymusidownloader

Open http://<host>:6874/ with http, not https.
EOF
