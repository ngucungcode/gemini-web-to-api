const $ = (id) => document.getElementById(id);
const status = (msg, cls) => {
  const el = $("status");
  el.textContent = msg;
  el.className = cls || "";
};

async function activeTab() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  return tab;
}

function authFromUrl(url) {
  const m = (url || "").match(/gemini\.google\.com\/u\/(\d+)\//);
  return m ? m[1] : "";
}

async function readCookies() {
  status("");
  const tab = await activeTab();
  const url = tab ? tab.url || "" : "";
  if (!/https:\/\/(.*\.)?gemini\.google\.com\//.test(url)) {
    status("Open a gemini.google.com tab first, then click Read.", "err");
    return;
  }
  const guessed = authFromUrl(url);
  if (guessed && !$("auth").value.trim()) $("auth").value = guessed;
  const all = await chrome.cookies.getAll({});
  const wanted = all.filter((c) => (c.domain || "").replace(/^\./, "").endsWith("google.com"));
  if (!wanted.length) {
    status("No google.com cookies found. Sign in first.", "err");
    return;
  }
  wanted.sort((a, b) => a.name.localeCompare(b.name));
  const header = wanted.map((c) => `${c.name}=${c.value}`).join("; ");
  $("out").value = header;
  const has1 = /__Secure-1PSID=/.test(header);
  const has2 = /__Secure-1PSIDTS=/.test(header);
  if (has1 && has2) {
    status(`OK: ${wanted.length} cookies. Tab slot: ${guessed || "(default)"}.`, "ok");
  } else {
    status("Header is missing __Secure-1PSID or __Secure-1PSIDTS. Reload Gemini and retry.", "err");
  }
}

async function copyOut() {
  const v = $("out").value.trim();
  if (!v) {
    status("Nothing to copy yet. Click Read cookies.", "err");
    return;
  }
  await navigator.clipboard.writeText(v);
  const name = ($("name").value.trim() || "acc1") + ".txt";
  status(`Copied. Save as cookies/${name} (auth slot: ${$("auth").value.trim() || "(default)"}).`, "ok");
}

function download() {
  const v = $("out").value.trim();
  if (!v) {
    status("Nothing to save yet. Click Read cookies.", "err");
    return;
  }
  const name = ($("name").value.trim() || "acc1") + ".txt";
  const blob = new Blob([v + "\n"], { type: "text/plain" });
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(a.href), 5000);
  const auth = $("auth").value.trim();
  status(auth ? `Saved ${name} (+ create ${name.replace(/\.txt$/, ".auth")} with "${auth}").` : `Saved ${name}.`, "ok");
}

async function sendToServer() {
  const base = $("server").value.trim().replace(/\/+$/, "");
  const v = $("out").value.trim();
  if (!base) {
    status("Set the server admin URL first.", "err");
    return;
  }
  if (!v) {
    status("Read cookies first.", "err");
    return;
  }
  status("Sending...");
  try {
    const r = await fetch(`${base}/admin/accounts`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name: $("name").value.trim() || "acc1",
        cookies: v,
        auth_user: $("auth").value.trim(),
      }),
    });
    const body = await r.text();
    if (!r.ok) status(`Server ${r.status}: ${body.slice(0, 200)}`, "err");
    else status("Server accepted the account.", "ok");
  } catch (e) {
    status(`Send failed: ${e.message}`, "err");
  }
}

document.addEventListener("DOMContentLoaded", () => {
  chrome.storage.local.get(["server"], (v) => {
    if (v.server) $("server").value = v.server;
  });
  $("server").addEventListener("change", () => {
    chrome.storage.local.set({ server: $("server").value.trim() });
  });
  $("read").addEventListener("click", readCookies);
  $("copy").addEventListener("click", copyOut);
  $("download").addEventListener("click", download);
  $("send").addEventListener("click", sendToServer);
  readCookies();
});
