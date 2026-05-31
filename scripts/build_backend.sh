#!/bin/bash
set -e

TARGET=$(rustc -vV | sed -n 's|host: ||p')
echo "building gitvis for $TARGET"

EXT=""
if [[ "$TARGET" == *"windows"* ]]; then EXT=".exe"; fi

cd "$(dirname "$0")/../backend"

go build -o "../frontend/src-tauri/binaries/gitvis-${TARGET}${EXT}" .

echo "done → frontend/src-tauri/binaries/gitvis-${TARGET}${EXT}"