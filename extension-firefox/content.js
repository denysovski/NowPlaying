// Reads the current song from YouTube / SoundCloud and reports it to background.js.
(() => {
  const host = location.hostname;
  const SOURCE = host.includes("soundcloud") ? "soundcloud" : "youtube";

  const text = (sel, attr) => {
    const el = document.querySelector(sel);
    if (!el) return "";
    return ((attr ? el.getAttribute(attr) : el.textContent) || "").trim();
  };

  function sessionMeta() {
    try {
      const m = navigator.mediaSession && navigator.mediaSession.metadata;
      if (m && m.title) return { title: m.title.trim(), artist: (m.artist || "").trim() };
    } catch (_) {}
    return null;
  }

  // "Artist - Topic", "ArtistVEVO" -> "Artist"
  function cleanChannel(name) {
    return name.replace(/\s*-\s*Topic$/i, "").replace(/VEVO$/i, "").trim();
  }

  // Drops noise like "(Official Video)" but keeps remix / feat. info.
  function cleanTitle(title) {
    return title
      .replace(/\s*[\(\[](official\s*(music\s*)?(video|audio|visualizer|lyric(s)?\s*video)|lyric(s)?(\s*video)?|audio|video|hd|hq|4k|visualizer|m\/?v)[\)\]]/gi, "")
      .replace(/\s{2,}/g, " ")
      .trim();
  }

  function readYouTube() {
    const isMusic = host === "music.youtube.com";
    const path = location.pathname;
    if (!isMusic && !path.startsWith("/watch") && !path.startsWith("/shorts")) return null;

    const media = isMusic
      ? document.querySelector("video")
      : document.querySelector("#movie_player video.html5-main-video") || document.querySelector("video");
    if (!media || !media.src) return null;

    let title = "", artist = "";
    const meta = sessionMeta();
    if (meta) {
      title = meta.title;
      artist = meta.artist;
    } else if (isMusic) {
      title = text("ytmusic-player-bar .title");
      artist = text("ytmusic-player-bar .byline a");
    } else {
      title = text("h1.ytd-watch-metadata yt-formatted-string") ||
              document.title.replace(/^\(\d+\)\s*/, "").replace(/\s*-\s*YouTube$/, "");
      artist = text("ytd-watch-metadata #owner #channel-name a") || text("#owner #channel-name a");
    }
    if (!title) return null;

    artist = cleanChannel(artist);
    title = cleanTitle(title);
    // "Artist - Song" titles: use the part before the dash as the artist.
    if (!isMusic) {
      const m = title.match(/^(.+?)\s+[-–—]\s+(.+)$/);
      if (m) { artist = m[1].trim(); title = m[2].trim(); }
    }
    const playing = !media.paused && !media.ended && media.readyState > 2;
    return { title, artist, playing };
  }

  function readSoundCloud() {
    let title = "", artist = "";
    const meta = sessionMeta();
    if (meta) {
      title = meta.title;
      artist = meta.artist;
    } else {
      title = text(".playbackSoundBadge__titleLink", "title") ||
              text(".playbackSoundBadge__titleLink span[aria-hidden]");
      artist = text(".playbackSoundBadge__lightLink", "title") || text(".playbackSoundBadge__lightLink");
    }
    if (!title) return null;

    const btn = document.querySelector(".playControls .playControl, .playControl");
    let playing;
    if (btn) playing = btn.classList.contains("playing");
    else playing = navigator.mediaSession && navigator.mediaSession.playbackState === "playing";
    // Link to the track in the player bar, not the page you're browsing.
    let url = location.href;
    const link = document.querySelector(".playbackSoundBadge__titleLink");
    if (link && link.getAttribute("href")) {
      const u = new URL(link.getAttribute("href"), location.origin);
      url = u.origin + u.pathname;
    }
    return { title, artist, playing: !!playing, url };
  }

  function read() {
    try {
      const r = SOURCE === "soundcloud" ? readSoundCloud() : readYouTube();
      if (!r) return { active: false, title: "", artist: "", playing: false, source: SOURCE, url: location.href };
      return { active: true, source: SOURCE, url: location.href, ...r };
    } catch (_) {
      return { active: false, title: "", artist: "", playing: false, source: SOURCE, url: location.href };
    }
  }

  let lastKey = "";
  let lastSent = 0;
  let alive = true;

  function report(force) {
    if (!alive) return;
    const s = read();
    const key = [s.active, s.title, s.artist, s.playing].join("\u0001");
    const now = Date.now();
    // send right away on any change, otherwise a heartbeat every 2s
    if (!force && key === lastKey && now - lastSent < 2000) return;
    lastKey = key;
    lastSent = now;
    try {
      const p = chrome.runtime.sendMessage({ type: "nowplaying", state: s });
      if (p && p.catch) p.catch(() => {});
    } catch (_) {
      alive = false; // extension was reloaded / removed
    }
  }

  // Fast paths: media events and title / player bar changes.
  const soon = () => setTimeout(report, 50);
  ["play", "playing", "pause", "ended", "emptied", "loadedmetadata", "durationchange"].forEach(ev =>
    document.addEventListener(ev, soon, true));
  const mo = new MutationObserver(soon);
  const titleEl = document.querySelector("title");
  if (titleEl) mo.observe(titleEl, { childList: true, characterData: true, subtree: true });
  if (SOURCE === "soundcloud") {
    const watchBar = () => {
      const bar = document.querySelector(".playControls");
      if (bar) mo.observe(bar, { childList: true, subtree: true, attributes: true, attributeFilter: ["class", "title", "href"] });
      else setTimeout(watchBar, 1000);
    };
    watchBar();
  }
  window.addEventListener("yt-navigate-finish", soon);

  // Safety net poll.
  setInterval(report, 500);
  report(true);
})();
