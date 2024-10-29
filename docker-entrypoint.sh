#!/bin/sh

# Function to validate worker type
validate_worker_type() {
    case "$1" in
        "ai"|"purge"|"stats")
            return 0
            ;;
        *)
            echo "Invalid worker type. Must be one of: ai, purge, stats"
            return 1
            ;;
    esac
}

# Function to validate worker subtype
validate_worker_subtype() {
    case "$1" in
        "friend"|"member"|"user"|"tracking"|"upload")
            return 0
            ;;
        *)
            echo "Invalid worker subtype. Must be one of: friend, member, user, tracking, upload"
            return 1
            ;;
    esac
}

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
        # Validate worker type and subtype
        if ! validate_worker_type "$WORKER_TYPE" || ! validate_worker_subtype "$WORKER_SUBTYPE"; then
            exit 1
        fi

        exec /app/bin/worker "$WORKER_TYPE" "$WORKER_SUBTYPE" --workers "$WORKERS_COUNT"
        ;;
    *)
        echo "Invalid RUN_TYPE. Must be either 'bot' or 'worker'"
        echo "Usage: RUN_TYPE=worker WORKER_TYPE=<type> WORKER_SUBTYPE=<subtype> WORKERS_COUNT=<count>"
        echo "Worker types: ai, purge, stats"
        echo "Worker subtypes: friend, member, user, tracking, upload"
        exit 1
        ;;
esac
