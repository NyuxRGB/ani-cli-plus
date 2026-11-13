# ani-cli-plus

ani-cli + Discord Rich Presence (works for Discord and Vesktop) + AniList sync.

I made this patch inspired by [curd](https://github.com/wraient/curd), without the extra features I didn't need. Think of it as a lightweight version: plain ani-cli with just Discord Rich Presence and AniList sync on top.
It also keeps using the same API / streaming source as ani-cli, unlike curd which relies on its own providers.

- `ani-cli-plus`: copy of `ani-cli/ani-cli` with `ani-cli-plus.patch` applied.
- `ani-tracker/`: small Go binary (no dependencies) started alongside mpv. It follows playback through mpv's IPC socket, updates Discord and marks the episode as watched on AniList once 85% of it has been played.

The anime is matched on AniList using the MAL id provided by ani-cli's source.

## Dependencies

Same as ani-cli:

- `curl`, `sed`, `grep`
- `fzf` (or `rofi` / `dmenu`)
- `mpv` (required for Discord / AniList features)
- `yt-dlp` or `ffmpeg` (only for `-d` downloads)
- `ani-skip` (optional, for `--skip`)

Added by ani-cli-plus:

- `go` ≥ 1.21 (build-time only, for `ani-tracker`)
- Discord or Vesktop running locally (for Rich Presence)
- an AniList account (for sync)
- `notify-send` / libnotify (optional, for AniList update notifications)
- `patch` (only to re-apply the patch after updating ani-cli)

## Installation
```
cd ani-tracker && go build -o ani-tracker . && cp ani-tracker ~/.local/bin/
cp ../ani-cli-plus ~/.local/bin/
```

Then use `ani-cli-plus` instead of `ani-cli` (same options and usage):
```
ani-cli-plus [name]
ani-cli-plus -c
```

## AniList

`ani-cli-plus --anilist-login`. Check with: `ani-tracker whoami`.

## Options

| Flag           | Environment variable        | Default    | Description                            |
| -------------- | --------------------------- | ---------- | -------------------------------------- |
| `--no-discord` | `ANI_CLI_DISCORD=0`         | `1`        | Disable Discord Rich Presence          |
| `--no-anilist` | `ANI_CLI_ANILIST=0`         | `1`        | Disable AniList sync                   |
|                | `ANI_CLI_ANILIST_THRESHOLD` | `85`       | % watched before AniList is updated    |
|                | `ANI_CLI_TITLE_LANG`        | `romaji`   | Title language (`romaji` or `english`) |
|                | `ANI_CLI_DISCORD_CLIENT_ID` | curd's app | Discord application ID                 |
|                | `ANI_CLI_NOTIFY=0`          | `1`        | Disable `notify-send` notifications    |
|                | `ANI_CLI_TRACKER`           | `$PATH`    | Path to `ani-tracker`                  |

Log: `~/.local/state/ani-cli/tracker.log`.

Only works with mpv (Linux/macOS), not VLC/IINA/Android/Windows.
Progress is never lowered, and an entry already marked COMPLETED is left untouched.

## Updating ani-cli

`-U` is disabled in the patched copy. Update `ani-cli/` then:
```
cp ani-cli/ani-cli ani-cli-plus && patch ani-cli-plus < ani-cli-plus.patch
```