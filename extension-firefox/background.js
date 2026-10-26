// Picks which tab's song to show and sends it to the NowPlaying app.
const SERVER = "http://127.0.0.1:8787/api/update";
const TAB_TIMEOUT = 6000; // a tab that stops reporting is forgotten

const tabs = new Map(); // tabId -> { state, seen, startedAt, lastPlayedAt }
let lastKey = "";
let lastPost = 0;

function pick() {
  const now = Date.now();
  let best = null;
  for (const [id, t] of tabs) {
    if (now - t.seen > TAB_TIMEOUT || !t.state.active) continue;
    if (!best) { best = t; continue; }
    // A playing tab always wins; among playing tabs the one that started last wins;
    // among paused tabs the one that played most recently wins.
    if (t.state.playing !== best.state.playing) {
      if (t.state.playing) best = t;
    } else if (t.state.playing ? t.startedAt > best.startedAt : t.lastPlayedAt > best.lastPlayedAt) {
      best = t;
    }
  }
  return best ? best.state : { active: false, title: "", artist: "", source: "", url: "", playing: false };
}

function publish(force) {
  const s = pick();
  const key = [s.active, s.title, s.artist, s.source, s.playing].join("\u0001");
  const now = Date.now();
  if (!force && key === lastKey && now - lastPost < 2500) return;
  lastKey = key;
  lastPost = now;
  fetch(SERVER, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(s),
  }).catch(() => { /* app not running */ });
}

chrome.runtime.onMessage.addListener((msg, sender) => {
  if (!msg || msg.type !== "nowplaying" || !sender.tab) return;
  const id = sender.tab.id;
  const now = Date.now();
  const prev = tabs.get(id);
  const s = msg.state;
  const songChanged = !prev || prev.state.title !== s.title || prev.state.artist !== s.artist;
  const startedNow = s.playing && (!prev || !prev.state.playing || songChanged);
  tabs.set(id, {
    state: s,
    seen: now,
    startedAt: startedNow ? now : (prev ? prev.startedAt : 0),
    lastPlayedAt: s.playing ? now : (prev ? prev.lastPlayedAt : 0),
  });
  publish(false);
});

chrome.tabs.onRemoved.addListener((id) => {
  if (tabs.delete(id)) publish(true);
});
