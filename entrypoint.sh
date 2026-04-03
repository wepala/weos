#!/bin/sh
set -e

# Install resource type presets (idempotent — skips existing types, updates schemas with --update)
/weos resource-type preset install website --update 2>&1 || true

# Start the server
exec /weos serve "$@"
