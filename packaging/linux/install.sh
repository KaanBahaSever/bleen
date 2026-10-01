#!/bin/sh
# Installs bleen for the current user (no root needed).
set -e
cd "$(dirname "$0")"
mkdir -p "$HOME/.local/bin" "$HOME/.local/share/applications" "$HOME/.local/share/icons/hicolor/512x512/apps"
install -m 755 bleen "$HOME/.local/bin/bleen"
install -m 644 bleen.png "$HOME/.local/share/icons/hicolor/512x512/apps/bleen.png"
sed "s|^Exec=bleen|Exec=$HOME/.local/bin/bleen|" bleen.desktop > "$HOME/.local/share/applications/bleen.desktop"
echo "bleen installed. Start it from your applications menu, or run: $HOME/.local/bin/bleen"
