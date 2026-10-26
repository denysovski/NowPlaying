# NowPlaying for OBS

**Show the song you're playing on YouTube or SoundCloud as a clean black & white card in OBS, and let your Twitch chat ask for it with `!song`.**

Made for Twitch streamers on Windows. It runs 100% locally on your PC: no account on a third-party site, no cloud service, and **no channel bot (Nightbot, StreamElements, …) required** for `!song`. The only online connection is your own (free) Twitch developer app, used so NowPlaying can answer in your chat.

<p align="center">
  <img src="docs/images/overlay.png" alt="OBS overlay: short and long (scrolling) song titles" width="720">
</p>

## Features

- **Instant updates.** Change the song and the card in OBS updates right away, with a smooth crossfade.
- **Auto-detection** of the song playing in your browser on **YouTube**, **YouTube Music** and **SoundCloud**, even when the player is in another tab or you're browsing a different SoundCloud page.
- **Full song names.** Titles that are too long scroll right to left so viewers can read the whole name.
- **Black & white design:** a coal-black card with small rounded corners and your own picture shown in black and white.
- **No flicker when skipping.** Short gaps between songs are ignored, and the card fades away only after 2 s of real silence.
- **Twitch `!song`** answered by NowPlaying itself (YouTube replies include the link; SoundCloud replies show just the name).
- **Dashboard app** with a dark theme: hide-when-paused, scroll speed and change picture are applied to OBS live.
- **Runs in the tray.** Closing the window keeps it running quietly in the background. It's light on resources: the dashboard page is unloaded while hidden.
- **Privacy option:** an OBS script starts NowPlaying with OBS and closes it when OBS closes, so nothing is detected off-stream.
- **Works with Chrome, Firefox**, Edge, Brave and Opera.

## How it works

```
 Browser tab (YouTube / SoundCloud)
        │  browser extension reads the song
        ▼
 NowPlaying.exe  ── local server on 127.0.0.1:8787 (never leaves your PC)
        │                     │
        ▼                     ▼
 OBS Browser Source      Twitch chat (!song) via your own Twitch app login
```

## Setup

Follow the steps in order. Steps 1 to 4 are required; 5 and 6 are optional but recommended.

| Step | What you do | Time |
| :---: | --- | :---: |
| **1** | [Download NowPlaying](#step-1--download) | 1 min |
| **2** | [Start the app](#step-2--start-the-app) | 1 min |
| **3** | [Install the browser extension](#step-3--install-the-browser-extension) (Chrome or Firefox) | 1–10 min |
| **4** | [Add the overlay to OBS](#step-4--add-the-overlay-to-obs) | 1 min |
| **5** | [Run NowPlaying only while OBS is open](#step-5--run-nowplaying-only-while-obs-is-open) *(recommended)* | 1 min |
| **6** | [Twitch `!song` command](#step-6--twitch-song-command) *(optional)* | 3 min |

<br>

---

<br>

## Step 1 — Download

Download the latest **NowPlaying.zip** from [Releases](https://github.com/denysovski/NowPlaying/releases/latest) and unzip it somewhere permanent, for example `Documents\NowPlaying`.

> [!TIP]
> Keep the folder where it is after setup. The browser extension, the OBS script and your picture are loaded from it.

<br>

---

<br>

## Step 2 — Start the app

Double-click **NowPlaying.exe**. The dashboard opens.

> [!NOTE]
> If Windows SmartScreen says *"Windows protected your PC"*, click **More info**, then **Run anyway**. The app isn't code-signed.

**Good to know:**

- Closing the window with **X** keeps NowPlaying running in the tray next to the clock.
- Click the tray icon to open the dashboard again. Right-click it and choose **Quit** to stop NowPlaying.
- The window uses Microsoft Edge WebView2, which is built into Windows 10 and 11.

<br>

---

<br>

## Step 3 — Install the browser extension

Pick your browser below. You only need one of them.

<br>

### 🟢 Chrome, Edge, Brave or Opera

1. Open `chrome://extensions` (Edge: `edge://extensions`, Opera: `opera://extensions`).
2. Turn on **Developer mode**.
3. Click **Load unpacked** and select the **`extension`** folder.
4. Reload any YouTube / SoundCloud tabs that were already open.

✅ Done, go to [Step 4](#step-4--add-the-overlay-to-obs).

<br>

### 🦊 Firefox

Firefox only keeps extensions that Mozilla has signed. Signing is free and automatic, and you only do it once.

**A. Prepare the file**

1. **Give the extension your own ID.** Open `extension-firefox/manifest.json` and change
   `"id": "nowplaying@change-me-to-your-name"` to something unique, e.g. `"nowplaying@yourname"`.
2. **Make the .xpi file.** Open the `extension-firefox` folder and select **all files inside it** (`manifest.json`, `background.js`, `content.js`, `icons`). Right-click, choose **Send to**, then **Compressed (zipped) folder**, and rename the zip to `NowPlaying-firefox.xpi`.

> [!IMPORTANT]
> Zip the files *inside* the folder, not the folder itself. If you skip the ID change, Mozilla answers *"Duplicate add-on ID found"*.

**B. Let Mozilla sign it**

3. Go to [addons.mozilla.org/developers](https://addons.mozilla.org/developers/) and log in (any Firefox account).
4. Click **Submit a New Add-on** and choose **On your own**. That keeps it private and unlisted.
5. Upload `NowPlaying-firefox.xpi`. If it asks whether you used a minifier or code generator, answer **No**, then submit.
6. Wait until the status says **Approved**. It usually takes a few minutes, and you also get an email.

**C. Install it**

7. Open **My Add-ons → NowPlaying for OBS → Manage Status & Versions**, click the version, then click the `.xpi` file. Firefox asks to install it: click **Add**.
8. Open `about:addons`, click **NowPlaying for OBS**, go to **Permissions**, and turn **all** permissions on.
9. Reload your YouTube / SoundCloud tabs.

✅ Done, go to [Step 4](#step-4--add-the-overlay-to-obs).

<br>

---

<br>

## Step 4 — Add the overlay to OBS

1. In OBS, click **Sources → + → Browser** and give it a name, e.g. *Now Playing*.
2. Leave **Local file** unticked.
3. Set **URL** to:
   ```
   http://127.0.0.1:8787/overlay
   ```
4. Set **Width 520** and **Height 130**, and clear the **Custom CSS** box.
5. Click **OK**, play a song, and move the card where you want it in your scene.

> [!NOTE]
> The card is invisible whenever nothing is playing or the app isn't running, so you can leave the source in your scene all the time.

<br>

---

<br>

## Step 5 — Run NowPlaying only while OBS is open

*Recommended.* NowPlaying starts quietly in the tray when OBS starts and closes a few seconds after OBS closes.

1. Keep `NowPlaying-OBS.lua` in the **same folder as NowPlaying.exe** (in the repository it's in `obs/`).
2. In OBS go to **Tools → Scripts**, click **+** and choose `NowPlaying-OBS.lua`.

> [!TIP]
> While NowPlaying isn't running, the extension sends nothing anywhere, so what you watch off-stream stays private.

<br>

---

<br>

## Step 6 — Twitch `!song` command

*Optional.* NowPlaying answers `!song` in your chat itself. Nightbot and StreamElements run on their own servers and can't see your PC, so you don't need them for this.

**A. Create a free Twitch app (one time)**

1. Go to [dev.twitch.tv/console/apps](https://dev.twitch.tv/console/apps), log in and click **Register Your Application**.
2. Fill it in:

   | Field | Value |
   | --- | --- |
   | Name | anything, e.g. `NowPlaying-yourname` |
   | OAuth Redirect URLs | `https://localhost` (required by Twitch but not used; remove any extra empty row) |
   | Category | Chat Bot |
   | Client Type | **Public** |

3. Click **Create**, then **Manage**, and copy the **Client ID**. You don't need a Client Secret.

**B. Connect NowPlaying**

4. In the dashboard, open **Twitch !song** at the bottom, paste the Client ID and click **Connect Twitch**.
5. `twitch.tv/activate` opens with a code. Check it matches the one in the app, log in with the account that should answer (your own or a bot account) and click **Authorize**.
6. The app shows **Listening in #yourchannel**. Type `!song` in your chat.

**Example replies**

```
🎵 Daft Punk - Something About Us → https://youtu.be/…
🎵 Fred again.. - Delilah
```

- You can change the command, the reply text, the "nothing is playing" text and the channel in the app.
- Reply variables: `{song}` `{title}` `{artist}` `{url}` `{source}` `{user}`.
- Replies are limited to one every 5 seconds.

> [!IMPORTANT]
> If you already have a `!song` command in Nightbot or StreamElements, delete it so chat doesn't get two answers.

The login renews itself. It's stored only on your PC in `%APPDATA%\NowPlaying\config.json`.

<br>

---

<br>

## Dashboard settings

| Setting | What it does |
| --- | --- |
| Hide when paused | The OBS card disappears while the song is paused |
| Scroll speed | Slow / Normal / Fast for long song names |
| Change picture | Opens the folder with `cover.jpg` selected. Replace it with your own image (`.jpg`, `.png`, `.webp`, `.gif`) and OBS updates within 2 seconds, shown in black and white automatically |
| Copy !song link | Copies `http://127.0.0.1:8787/api/now-playing?format=text` |

The current song is also written to `nowplaying.txt` next to the exe, for an OBS Text source or other tools.

## Local API

| Endpoint | Returns |
| --- | --- |
| `GET /overlay` | The OBS overlay page |
| `GET /api/now-playing` | Current song as JSON |
| `GET /api/now-playing?format=text` | `Artist - Title` |
| `GET /events` | Server-Sent Events stream used by the overlay |

Everything listens on `127.0.0.1` only, so nothing is reachable from other computers.

## Project structure

```
app/                 Windows app: local server, dashboard window, tray, Twitch chat
  web/               dashboard (app.html), OBS overlay (overlay.html), default cover
extension/           browser extension for Chrome / Edge / Brave / Opera
extension-firefox/   the same extension packaged for Firefox
obs/                 OBS Lua script (start/stop NowPlaying with OBS)
docs/images/         screenshots
```

## Built with

- **Go:** the Windows app, local web server, tray icon and Twitch chat bot (WebView2 window via [go-webview2](https://github.com/jchv/go-webview2))
- **JavaScript:** the browser extension (Manifest V3), the overlay and the dashboard logic
- **HTML & CSS:** the OBS overlay and the dashboard design
- **Lua:** the OBS script

## Keywords

OBS, now playing, Twitch, streamer, song overlay, `!song` command, YouTube, SoundCloud, browser source, Windows, Chrome extension, Firefox add-on
