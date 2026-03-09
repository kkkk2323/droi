#!/bin/bash
set -euo pipefail

pnpm install --frozen-lockfile 2>/dev/null || pnpm install

mkdir -p .factory/tmp /tmp/droi-mission-e2e

if [ ! -f /tmp/droi-mission-e2e/app-state.json ]; then
  if [ -f "$HOME/Library/Application Support/droi/app-state.json" ]; then
    cp "$HOME/Library/Application Support/droi/app-state.json" /tmp/droi-mission-e2e/app-state.json
  elif [ -f "$HOME/.droid-app/app-state.json" ]; then
    cp "$HOME/.droid-app/app-state.json" /tmp/droi-mission-e2e/app-state.json
  fi
fi
