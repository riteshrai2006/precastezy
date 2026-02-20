# Frontend (assets only) — Precast Web

**Live site:** https://precast.blueinvent.com  

This folder is for **frontend only**. The backend (`api` on the server) is not modified by anything here.

## Server layout (do not change `api`)

On the server at `18.140.23.205`:

| Path | Purpose |
|------|--------|
| `/var/www/precast.blueinvent.com/` | Web root (nginx serves from here) |
| `/var/www/precast.blueinvent.com/api` | **Backend — do not touch** |
| `/var/www/precast.blueinvent.com/assets` | **Frontend — work here only** |
| `/var/www/precast.blueinvent.com/index.html` | SPA entry (frontend) |
| `/var/www/precast.blueinvent.com/firebase-messaging-sw.js` | Service worker (frontend) |

## Workflow

### Option 1: Upload dist zip and run update_react.sh (recommended for new builds)

If you have a new `dist 2.zip` (or any dist zip) from your build:

```bash
cd frontend
export FRONTEND_SSH_KEY="$HOME/path/to/jaildev.pem"  # if needed
./upload-and-update-react.sh [path-to-zip]
# Default: uploads ~/Downloads/dist\ 2.zip
```

This will:
1. Upload the zip to the server
2. Run `update_react.sh` on the server (which extracts and deploys it)

### Option 2: Pull, edit, and deploy manually

1. **Pull** current frontend from server (to debug or edit locally):
   ```bash
   cd frontend
   ./pull-from-server.sh
   ```
2. **Edit** only under `assets/` (and root files like `index.html` if needed). Do not change anything that deploys to `api/`.
3. **Deploy** only frontend back to server:
   ```bash
   ./deploy-assets-only.sh
   ```

## SSH

Use your key when prompted by the scripts (e.g. `-i jaildev.pem`). Set the key path if needed:

```bash
export FRONTEND_SSH_KEY="$HOME/path/to/jaildev.pem"
./pull-from-server.sh
./deploy-assets-only.sh
```

## Debugging

- First run `pull-from-server.sh` so you have the live `assets` and `index.html` locally.
- Reproduce any issue locally if possible (e.g. serve `index.html` and `assets/` with a static server).
- All changes in this repo under `frontend/` are frontend-only; deploy with `deploy-assets-only.sh` so the backend is never modified.
