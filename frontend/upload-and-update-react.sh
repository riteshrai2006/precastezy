#!/bin/bash
# Upload dist zip file and run update_react.sh on server
# Usage: ./upload-and-update-react.sh [path-to-zip]
set -e

SERVER="${FRONTEND_SERVER:-ubuntu@18.140.23.205}"
REMOTE_DIR="/var/www/precast.blueinvent.com"
SSH_OPTS="-o StrictHostKeyChecking=accept-new"
[[ -n "${FRONTEND_SSH_KEY}" ]] && SSH_OPTS="$SSH_OPTS -i ${FRONTEND_SSH_KEY}"

# Default to "dist 2.zip" in Downloads if not provided
ZIP_FILE="${1:-$HOME/Downloads/dist 2.zip}"
ZIP_FILE="${ZIP_FILE/#\~/$HOME}"  # Expand ~ if present

if [[ ! -f "$ZIP_FILE" ]]; then
  echo "Error: Zip file not found: $ZIP_FILE"
  echo "Usage: $0 [path-to-zip-file]"
  echo "Default: ~/Downloads/dist 2.zip"
  exit 1
fi

echo "Uploading $ZIP_FILE to server..."
# Upload to web directory where update_react.sh can find it (as dist.zip)
scp $SSH_OPTS "$ZIP_FILE" "${SERVER}:${REMOTE_DIR}/dist.zip"

echo ""
echo "Running update_react.sh on server..."
ssh $SSH_OPTS "$SERVER" << ENDSSH
cd ${REMOTE_DIR}
chmod +x update_react.sh
./update_react.sh
ENDSSH

echo ""
echo "Done! Frontend updated."
