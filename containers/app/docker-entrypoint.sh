#!/bin/sh

set -e

echo "Running '$1'..."

if [ "$1" = "server" ]; then
    [ -x /app/server ] || { echo "Binary /app/server not found or not executable"; exit 1; }
    exec /app/server
elif [ "$1" = "worker" ]; then
    [ -x /app/worker ] || { echo "Binary /app/worker not found or not executable"; exit 1; }
    exec /app/worker
else
    echo "Unknown command: $1"
    exit 1
fi
