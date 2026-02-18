# Setting Up Git Authentication on Server

If `git fetch` fails with authentication errors, your repo is **private** and the server needs credentials.

## Option 1: SSH Key (Recommended)

**On the server** (SSH into it):

```bash
# 1. Generate SSH key (if not exists)
ssh-keygen -t ed25519 -C "server@precast" -f ~/.ssh/id_ed25519 -N ""

# 2. Display public key
cat ~/.ssh/id_ed25519.pub
```

**On GitHub:**
1. Go to: https://github.com/riteshrai2006/precastezy/settings/keys
2. Click "Add deploy key" or "Add SSH key"
3. Paste the public key from step 2
4. Save

**Update server-deploy.sh:**
Change line 19 in `server-deploy.sh`:
```bash
GIT_REPO="${GIT_REPO:-git@github.com:riteshrai2006/precastezy.git}"
```

Then upload the updated script to server.

---

## Option 2: HTTPS with Personal Access Token

**On GitHub:**
1. Go to: https://github.com/settings/tokens
2. Click "Generate new token (classic)"
3. Name: "Server Deploy"
4. Scopes: Check `repo` (full control of private repositories)
5. Generate and **copy the token** (you won't see it again!)

**On your Mac** (edit `server-deploy.sh`):
Change line 19:
```bash
GIT_REPO="${GIT_REPO:-https://YOUR_TOKEN_HERE@github.com/riteshrai2006/precastezy.git}"
```

Replace `YOUR_TOKEN_HERE` with the token you copied.

Then upload script to server:
```bash
cd /Users/riteshrai/Documents/precastezy
./upload-and-deploy.sh
```

---

## Option 3: Make Repo Public (Easiest)

If the repo doesn't need to be private:
1. Go to: https://github.com/riteshrai2006/precastezy/settings
2. Scroll to "Danger Zone"
3. Click "Change visibility" → "Make public"

Then HTTPS will work without authentication.

---

## Test After Setup

On server:
```bash
cd /home/ubuntu/precast-backend
git fetch origin
```

Should work without errors.
