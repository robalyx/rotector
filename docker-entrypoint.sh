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

case "$1" in
    "bot")
        exec /app/bin/bot
        ;;
    "worker")
        if [ -z "$2" ] || [ -z "$3" ]; then
            echo "Usage: worker <type> <subtype> [workers_count]"
            exit 1
        fi

        WORKER_TYPE="$2"
        WORKER_SUBTYPE="$3"
        WORKERS_COUNT="${4:-1}"  # Default to 1 if not specified

        # Validate worker type and subtype
        if ! validate_worker_type "$WORKER_TYPE" || ! validate_worker_subtype "$WORKER_SUBTYPE"; then
            exit 1
        fi

        exec /app/bin/worker "$WORKER_TYPE" "$WORKER_SUBTYPE" --workers "$WORKERS_COUNT"
        ;;
    *)
        echo "Usage: { bot | worker <type> <subtype> [workers_count] }"
        echo "Worker types: ai, purge, stats"
        echo "Worker subtypes: friend, member, user, tracking, upload"
        exit 1
        ;;
esac
