#!/bin/bash
# Export RFPs from PHP demo SQLite database to JSON format
# Usage: ./scripts/export-php-demo.sh [path-to-demo] > rfps.json

DEMO_PATH="${1:-../rfp-demo}"
DB_PATH="$DEMO_PATH/storage/db/app.sqlite"

if [ ! -f "$DB_PATH" ]; then
    echo "Error: Database not found at $DB_PATH" >&2
    exit 1
fi

# Export RFPs as JSON array
sqlite3 -json "$DB_PATH" "
SELECT
    title,
    owner_agency as agency,
    location_city as city,
    location_state as state,
    listing_url as source_url,
    portal,
    publish_date as posted_date,
    due_date,
    category,
    venue_type,
    term_months,
    est_value,
    incumbent,
    login_required
FROM rfps
ORDER BY created_at DESC
"
