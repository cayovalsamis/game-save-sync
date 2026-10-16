#!/bin/bash
# Installs savesync on a Steam Deck as a systemd user service. Everything
# lives in the home folder, so SteamOS updates leave it alone.
# Usage: scripts/install-deck.sh <path to the Linux savesync binary>
set -euo pipefail

bin="${1:?usage: install-deck.sh <savesync binary>}"
unit_dir="$HOME/.config/systemd/user"

systemctl --user stop game-save-sync.service 2>/dev/null || true
install -Dm755 "$bin" "$HOME/.local/bin/savesync"

mkdir -p "$unit_dir"
cat > "$unit_dir/game-save-sync.service" <<'EOF'
[Unit]
Description=Sync non-Steam game saves with the PC
After=network-online.target

[Service]
ExecStart=%h/.local/bin/savesync run
Restart=always
RestartSec=30

[Install]
WantedBy=default.target
EOF

systemctl --user daemon-reload
systemctl --user enable --now game-save-sync.service
systemctl --user --no-pager status game-save-sync.service | head -5
echo "Log: ~/.local/state/game-save-sync/savesync.log"
