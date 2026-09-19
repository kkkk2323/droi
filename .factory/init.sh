#!/bin/bash
set -euo pipefail

pnpm install --frozen-lockfile 2>/dev/null || pnpm install
pnpm exec playwright install chromium
