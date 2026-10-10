# Gemini Cookies Exporter (1 extension, N accounts)

One extension handles every account. Repeat per browser profile/tab.

1. Go to `chrome://extensions`, enable Developer mode, **Load unpacked** → `extension/`.
2. Open `https://gemini.google.com` signed into account 1 (check the tab URL: `/u/2/` means slot `2`).
3. Click the extension → **Read cookies** → **Save .txt** as `acc1.txt` into this repo's `cookies/`.
   - Same-tab rule: cookie + slot must come from the same tab.
   - Slot file: when the URL has `/u/2/`, the auth slot is `2`. Either type it in the popup, or create `cookies/acc1.auth` containing `2`. No `/u/N/` → leave empty.
4. Switch Google account (or another Chrome profile), repeat as `acc2.txt`, `acc3.txt`, ...
5. Restart `gemini-web-to-api` (or `POST /admin/accounts`). Check `GET /health/accounts`.

No DevTools Network copy-paste. The popup reads the active tab's cookies via the `cookies` API, sorts them, validates `__Secure-1PSID` + `__Secure-1PSIDTS`, and can push directly to the server.
