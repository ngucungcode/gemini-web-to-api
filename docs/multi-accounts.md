# Multi-account cookies (1 extension, N accounts)

The server holds a pool of Gemini Web accounts and round-robins requests across
healthy ones. A failing/quota-hit account is skipped with a short backoff; the
next account serves the request. No request code changes: OpenAI / Claude /
Gemini endpoints behave the same.

## Add cookies the easy way (recommended)

Use the bundled extension — one install covers all accounts:

1. `chrome://extensions` → Developer mode → **Load unpacked** → `extension/`.
2. Open `https://gemini.google.com` signed into account 1.
3. Extension popup → **Read cookies** → **Save .txt** → drop it in `cookies/` as `acc1.txt`.
   - Same-tab rule: cookie and slot must come from the same tab.
   - Slot: if the tab URL has `/u/2/`, the auth slot is `2`. Type it in the popup, or write `cookies/acc1.auth` containing `2`. No `/u/N/` → leave empty.
4. Switch account (or Chrome profile) → repeat as `acc2.txt`, `acc3.txt`, ...
5. Restart the server (or `POST /admin/accounts`). Verify `GET /health/accounts`.

File layout:

```text
cookies/
  acc1.txt        # full Cookie header for account 1
  acc1.auth       # optional: "2"
  acc2_u1.txt     # suffix form: auth slot parsed from filename
  accounts.json   # optional alternative: [{"name":"acc1","cookies":"...","auth_user":"2"}]
```

Notes:

- `cookies/*.txt` = easiest. One header per file, no escaping.
- `accounts.json` = same dir, friendlier for automation. `cookies/.cache/` holds rotation state — do not hand-edit it.
- Env still works: `GEMINI_COOKIES` (+`GEMINI_AUTH_USER`), `GEMINI_COOKIES_2` (+`GEMINI_AUTH_USER_2`), ... up to `_10`, plus `GEMINI_ACCOUNTS_JSON`. Env accounts load first, files next; duplicates (same `__Secure-1PSID`) are merged.
- Invalid files fail fast at boot with the filename in the error.

## Runtime management

| Call | Effect |
| ---- | ------ |
| `GET /health/accounts` | pool status: name, healthy, models, masked PSID |
| `GET /admin/accounts` | same as above |
| `POST /admin/accounts {"name":"acc3","cookies":"...","auth_user":"2"}` | validate + init + add (persisted to `cookies/`) |
| `POST /admin/accounts {"name":"acc1","cookies":"...","replace":true}` | rotate one account's cookies |
| `DELETE /admin/accounts/acc3` | drop + close (removes persisted file; env-defined accounts return after restart) |

Set `ADMIN_TOKEN` to require `Authorization: Bearer <token>` (or `X-Admin-Token` / `?token=`) on `/admin/*`. Without it, local admin routes are open — bind to localhost or set the token.

## Behavior notes

- Requests fail over to the next healthy account automatically.
- `429/quota` → account cools down ~5 min; auth errors ~10 min; other errors ~30 s. Healthy-first ordering resumes after cooldown.
- Model list = union of models across accounts; a model usable on any account resolves.
- Each account keeps its own `__Secure-1PSIDTS` rotation cache under `cookies/.cache/`.
- Deep research and image uploads ride the same pool.
