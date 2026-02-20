#!/bin/bash
# Deploy only frontend to the server. Backend (api) is never touched.
# Pushes: assets/, index.html, firebase-messaging-sw.js, precast.svg, vite.svg
set -e
SERVER="${FRONTEND_SERVER:-ubuntu@18.140.23.205}"
REMOTE="/var/www/precast.blueinvent.com"
SSH_OPTS="-o StrictHostKeyChecking=accept-new"
[[ -n "${FRONTEND_SSH_KEY}" ]] && SSH_OPTS="$SSH_OPTS -i ${FRONTEND_SSH_KEY}"

if [[ ! -d "assets" ]]; then
  echo "Run this script from the frontend directory (where assets/ lives)."
  exit 1
fi

echo "Deploying frontend only to ${SERVER}:${REMOTE}"
echo "  Pushing: assets/, index.html, firebase-messaging-sw.js, precast.svg, vite.svg"
echo "  Not touching: api/ (backend)"
echo ""

rsync -avz --progress -e "ssh $SSH_OPTS" \
  ./assets/ \
  "${SERVER}:${REMOTE}/assets/"
for f in index.html firebase-messaging-sw.js precast.svg vite.svg; do
  if [[ -f "$f" ]]; then
    rsync -avz -e "ssh $SSH_OPTS" "$f" "${SERVER}:${REMOTE}/"
  fi
done

echo "Done. Backend (api) was not touched."
