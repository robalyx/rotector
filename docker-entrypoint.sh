#!/bin/sh

# Use environment variables with defaults
RUN_TYPE="${RUN_TYPE:-bot}"
WORKER_TYPE="${WORKER_TYPE:-ai}"
WORKER_SUBTYPE="${WORKER_SUBTYPE:-friend}"
WORKERS_COUNT="${WORKERS_COUNT:-1}"

case "$RUN_TYPE" in
    "bot")
        exec /app/bin/bot
        ;;
    "worker")
        exec /app/bin/worker "$WORKER_TYPE" "$WORKER_SUBTYPE" --workers "$WORKERS_COUNT"
        ;;
    *)
        echo "Invalid RUN_TYPE. Must be either 'bot' or 'worker'"
        echo "Usage: RUN_TYPE=worker WORKER_TYPE=<type> WORKER_SUBTYPE=<subtype> WORKERS_COUNT=<count>"
        exit 1
        ;;
esac