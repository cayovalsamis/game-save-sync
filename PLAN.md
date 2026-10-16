# game-save-sync: status and plan

Steam Cloud-style save sync for **non-Steam games** between a Windows PC and a
Steam Deck, through Google Drive. This file is the handoff document: read it
fully before changing anything, and update it when you finish an item.

## How it works (one paragraph)

Each device runs `savesync run` in the background. It watches the game folder
for running games, and when a game closes it backs up the save with
**Ludusavi** and uploads it with **rclone** to Google Drive. Every 5 minutes,
and after wake from sleep, it checks Drive for newer saves from the other
device and restores them, never while that game is running. The core rule
(`internal/syncer/syncer.go`, `decide`) compares the live save and the cloud
head with what they were at the last sync (`state.json`): whichever side
changed moves to the other. If both changed, the newer save wins and the other
is kept: the cloud's as an older backup generation, the local one zipped into
`conflicts/`. Folder names identify games across devices, mapped to Ludusavi
titles (`internal/resolve`).

## Devices and paths

| | Windows PC | Steam Deck (SteamOS 3.8, user `deck`) |
|---|---|---|
| Games | `G:\local_games\<Game>\` (run by .exe, no launcher) | `/run/media/deck/9cd093b0-a208-4bfa-b57c-68c14784f89f/Games/<Game>/`, added to Steam as non-Steam shortcuts, run through Proton |
| savesync binary | `%LOCALAPPDATA%\game-save-sync\bin\savesync.exe` (console) and `savesyncw.exe` (no window, used by the task) | `~/.local/bin/savesync` |
| Autostart | Task Scheduler task `game-save-sync` (at logon) | systemd user service `game-save-sync.service` |
| savesync config | `%APPDATA%\game-save-sync\config.json` | `~/.config/game-save-sync/config.json` |
| State, log, conflicts | `%LOCALAPPDATA%\game-save-sync\` (`state.json`, `savesync.log`, `conflicts\`, `device-summary.json`) | `~/.local/state/game-save-sync/` (plus `ludusavi/`, the managed Ludusavi config) |
| Ludusavi | 0.31.0 via winget; config `%APPDATA%\ludusavi\config.yaml`; backups `C:\Users\Cayo\ludusavi-backup` | 0.31.0 Flatpak `com.github.mtkennerly.ludusavi`; base config `~/.var/app/com.github.mtkennerly.ludusavi/config/ludusavi/`; backups `~/ludusavi-backup` |
| rclone | 1.75.1 via winget; `%APPDATA%\rclone\rclone.conf` | 1.75.1 at `~/.local/bin/rclone`; `~/.config/rclone/rclone.conf` (copy of the PC's) |

**Cloud layout:** remote `gdrive:` (Google Drive, scope `drive.file`, using the
user's own OAuth client "game-save-sync" in Google Cloud, published to
production). `gdrive:game-save-sync/ludusavi/<escaped title>/` holds Ludusavi
backups. `gdrive:game-save-sync/devices/<device>.json` holds each device's game
list for the `games` table. The privacy policy and homepage required by Google
are in `docs/` (GitHub Pages). Credentials live only in the rclone.conf files,
never in this repo.

**Path translation:** a backup records the paths of the device that made it.
- **Windows:** global restore redirects in Ludusavi's own config, added on
  demand (`redirect.WindowsRules`, `redirect.Ensure`).
- **Deck:** the same Windows path must go to a different Proton prefix for
  each game. So savesync runs Ludusavi with `--config
  ~/.local/state/game-save-sync/ludusavi`, a managed copy of the base config
  that it rewrites before each restore. The copy adds `otherWine` roots for
  every game prefix and only the current game's redirects
  (`redirect.WriteManaged`, `internal/app/deck.go`). Each folder's prefix is
  found through the shortcut whose Exe points into it (`internal/steam`).
- **Restore guards:** a restore is refused if any file would not land in a
  native path (`redirect.IsNative`), or if the downloaded backup is incomplete
  (`Mapping.Verify`).

## Working on it

- **Code:** `C:\Users\Cayo\game-save-sync` (WSL path
  `/mnt/c/Users/Cayo/game-save-sync`), pushed to
  https://github.com/cayovalsamis/game-save-sync (public). Go 1.27 is in WSL at
  `~/.local/go/bin`.
- **Checks:** `go vet ./... && GOOS=windows go vet ./... && go test ./...`
- **Build:**
  - `GOOS=windows go build -o bin/savesync.exe ./cmd/savesync`
  - `GOOS=windows go build -ldflags "-H windowsgui" -o bin/savesyncw.exe ./cmd/savesync`
  - `CGO_ENABLED=0 go build -o bin/savesync-linux ./cmd/savesync`
  - Use `GOFLAGS=-buildvcs=false` for all three.
- **Deploy to Windows:** `powershell.exe -ExecutionPolicy Bypass -File 'C:\Users\Cayo\game-save-sync\scripts\install-windows.ps1'`. It stops the task, copies the binaries and restarts it.
- **Deploy to the Deck:** `scp bin/savesync-linux scripts/install-deck.sh deck:/tmp/ && ssh deck 'bash /tmp/install-deck.sh /tmp/savesync-linux'`
- **Deck access:** `ssh deck` from WSL, an alias for `deck@192.168.1.244` with key `~/.ssh/deck_savesync`. The Deck is often asleep. If it's unreachable, ask the user to wake it and keep it in Desktop Mode. The user's main key `~/.ssh/id_ed25519` has a passphrase; don't use it.
- **Pushing:** SSH push needs that passphrase, so push over HTTPS:
  `git -c credential.helper= -c credential.helper='!gh auth git-credential' push origin main`
- **CLI:**
  - `savesync games [-refresh]`: the table.
  - `savesync sync`: one pass, now.
  - `savesync publish`: upload this device's game list only.
  - `savesync wrap -- %command%`: Deck launch option; syncs one game, then starts it.

**Safety rules for manual work:**
- Never restore while the game is running.
- Before any manual restore, tar the live save into `~/save-sync/safety/` (Deck) or `C:\Users\Cayo\save-sync\safety\` (PC).
- Never delete cloud backups. Ludusavi keeps 5 generations (`retention.full: 5`).
- Saves belong to the user. When unsure, archive rather than overwrite.

## Done and verified

- Ludusavi and rclone are installed and configured on both devices. Drive runs on the user's own OAuth client.
- Windows daemon:
  - Process watch under `G:\local_games`, with a 15 s grace period after exit.
  - Upload on exit.
  - Background pull every 5 min, after resume from sleep (detected by a clock jump), and retries every 1 min when offline.
  - Single instance, logon task, no console window.
- Deck daemon:
  - Process watch through `/proc`, including Proton `Z:\` paths.
  - Per-game prefix redirects.
  - Systemd user service.
  - `wrap` command.
- `savesync games` table across both devices, built from the per-device summaries in Drive.
- Real transfers verified byte for byte: Deck→PC and PC→Deck (Orc Incremental), and PC→Deck into a never-launched prefix (Dressmaker; the user confirmed the progress was there).
- Tests:
  - The decision table (`syncer/decide_test.go`, 15 cases).
  - Redirects, `Verify` (also run against all real PC backups), name cleaning, folder detection.

## Open bugs (do these first)

1. **The process watcher stops while a sync runs.** `daemon.Run` polls processes in the same loop that syncs. On the Deck, the user played Orc Incremental while Dressmaker was uploading (about 1 min) and the session was never seen as running. A full pass could also back up or restore a game that is running unnoticed (restores re-check `IsRunning`; uploads don't).
   - **Fix:** run the watcher in its own goroutine. Keep the running set and exit events behind a mutex, or send them on a channel. Have `sync()` build `skip` from the live set. Have `push` re-check `IsRunning` right before `Backup`, as `pull` does before `Restore`.
2. **The table credits the save to the wrong device.** Restored files keep their original modification time, so after a pull this device's live save has the same timestamp as the other device's. Ties go to whichever device the table runs on: the PC said "Windows PC" for a Deck session.
   - **Fix:** add `Origin` (a device name) to `state.Game`. Set it to this device on push. On pull, set it to the device that made the backup (match `Backup.OS` against the device summaries' `os` field). In `GamesTable`, when the live fingerprint equals the recorded one, label this device's save time with `Origin`. The device summary can carry it too.
3. **Both devices uploading the same game at once can mix the cloud folder.** This happens if both played offline and came online together. `Verify` stops a restore from a mixed folder, but the cloud can stay inconsistent until one side uploads again.
   - **Option:** before upload, write a lock marker `gdrive:game-save-sync/locks/<title>` with device and time, and skip pulls of a locked game. Or upload to a temporary name and rename. Investigate how `ludusavi cloud upload` orders its writes.
4. **Persistent per-game failures retry every minute forever** (`RetryMinutes`). Back off per game, e.g. 1, 5, then 30 minutes, and reset on success.

## Next features

5. **Discovery mode for unmatched folders.** Ludusavi doesn't know these folders, so their saves aren't synced:
   - PC: Brave.New.Wonders, Fly Corp, I Know a Guy, Mars.Attracts, Nocturne, Tasty Chef, The Loopler, Tiny Eden, Walk of Life, Warhammer.40000.Boltgun.2.
   - Deck: BALLxPIT, Date Everything, The LotR - Return to Moria, What The Car, Warhammer.40000.Boltgun.2.

   **Idea:** `savesync discover <folder>` snapshots file mtimes under AppData (Roaming, Local, LocalLow), Documents, Saved Games, Public Documents and the game folder. The user plays, saves and quits. It then shows the changed files and, once the user confirms, writes a Ludusavi `customGames` entry using Ludusavi path placeholders (`<winAppData>`, `<home>`, `<base>`) so the entry works on both devices.

   **Checks:**
   - Custom game names must match on both devices.
   - On the Deck, the managed config is copied from the base config, so put custom games in the base config.
   - A Steam ID found but missing from the database (Fly Corp, Nocturne) deliberately does **not** fall back to name matching, because "Nocturne" by name is a different game.
6. **Notifications.** Tell the user about conflicts, repeated failures and "can't sync: add a Steam shortcut".
   - **Windows:** toast notifications. A tray icon with "sync now / pause / open log" is optional.
   - **Deck:** desktop notifications don't show in Game Mode; research the options.
7. **Faster uploads.**
   - **Now:** a push takes about 17–60 s: Ludusavi `cloud download`, then `backup`, then `cloud upload`. The download is there only so the cloud's older generations survive the upload.
   - **Option A:** rclone `copy` just the new backup folder plus `mapping.yaml`. This needs care, because `mapping.yaml` must list all generations.
   - **Option B:** skip the download when the local backup folder already matches the cloud head (it usually does).
8. **Wrapper adoption on the Deck.**
   - **The gap:** without the launch option, a game started within about a minute of waking can run on an old save before the background pull finishes.
   - **Options:** document it per game, or have `savesync install-launch-options` set `LaunchOptions` in `shortcuts.vdf`. That is only safe while Steam is not running, because Steam rewrites the file; it would need a binary VDF writer.
   - **Status:** the user hasn't enabled the wrapper on any game yet.
9. **Games without a Steam shortcut on the Deck** are skipped with the status "not synced (no Steam shortcut)". Currently that's The Adventures of Elliot. Tell the user (see 6). Adding shortcuts automatically is possible but intrusive.
10. **Registry-based saves** (Windows registry ↔ Proton `user.reg`) are not handled. Ludusavi can back up registry saves; translating them across devices is unexplored.

## Smaller items and known limits

- Windows: games running elevated can't be inspected (`OpenProcess` fails), so they're never seen as running. A fallback could match the process image name against the `.exe` files in each game folder.
- `Mapping.Verify` skips zip-format backups. Ludusavi is set to `simple`.
- The Deck's SD card UUID is in the PC's `peerGamesRoots`. If the card changes, update it, or derive it from the Deck's device summary by adding `gamesRoot` to `devices.Summary`.
- Both the PC's Ludusavi config (`G:/local_games` root, Deck prefix redirects) and the Deck's base config (`Games` root) were edited for this project. Originals are kept next to them as `config.yaml.orig`.
- Old leftovers in Drive: the first `game-save-sync` folder, created with rclone's retired shared client, is invisible to the app. The user may delete it from the Drive web UI (the older of the two folders with that name).
- SSH on the Deck (`sshd`) is enabled for development. Remind the user to disable it when development ends: `sudo systemctl disable --now sshd`.
- Tests are missing for `push`, `pull` and `resolveBoth` (they need fakes for the Ludusavi client and the cloud) and for `steam.ReadShortcuts` (a synthetic binary VDF fixture would do; don't commit the user's real file).

## Pending checks with the user

- A full round trip after bugs 1 and 2 are fixed:
  1. Play on the Deck and quit; the PC should show "newer save in cloud", then pull it.
  2. Play on the PC; the Deck should pull it.

  Before the fixes, the Deck→PC round trip did reach the PC, but only through a conflict and a failed partial restore. It is fixed now by `Verify`; see bug 3 for the remaining race.
