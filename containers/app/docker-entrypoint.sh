#!/bin/sh

set -e

echo "Running $1..."

if [ "$1" = 'server' ]; then
    exec /app/server
elif [ "$1" = 'worker' ]; then
    exec /app/worker
fi
