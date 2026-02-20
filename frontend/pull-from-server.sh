#!/bin/bash
# Pull frontend (assets + root files) from live server.
# Does not touch backend (api). Safe to run anytime.
set -e
SERVER="${FRONTEND_SERVER:-ubuntu@18.140.23.205}"
REMOTE="/var/www/precast.blueinvent.com"
SSH_OPTS="-o StrictHostKeyChecking=accept-new"
[[ -n "${FRONTEND_SSH_KEY}" ]] && SSH_OPTS="$SSH_OPTS -i ${FRONTEND_SSH_KEY}"

echo "Pulling frontend from ${SERVER}:${REMOTE} into $(pwd)"
echo "  (assets + index.html + firebase-messaging-sw.js + precast.svg + vite.svg)"
echo ""

mkdir -p assets
rsync -avz --progress -e "ssh $SSH_OPTS" \
  "${SERVER}:${REMOTE}/assets/" \
  "./assets/"
rsync -avz -e "ssh $SSH_OPTS" \
  "${SERVER}:${REMOTE}/index.html" \
  "${SERVER}:${REMOTE}/firebase-messaging-sw.js" \
  "${SERVER}:${REMOTE}/precast.svg" \
  "${SERVER}:${REMOTE}/vite.svg" \
  ./

echo "Done. Backend (api) was not touched."
