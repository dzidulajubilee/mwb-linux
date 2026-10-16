# mwb-gui

A settings and status window for mwb-linux. It edits
`~/.config/mwb/config.toml` and controls the `mwb` user service. The `mwb`
client itself stays a single binary with no GUI dependencies.

```bash
sudo apt install python3-gi python3-gi-cairo gir1.2-gtk-4.0
make install-gui      # per-user, no sudo
```

Then open **Mouse Without Borders** from your apps, or run `mwb-gui`.
Use `mwb-gui --config PATH` to edit a different file.

## What it does

- **Screen arrangement.** Click the side the other computer is on. This sets
  `edge`.
- **Connection.** Pick the other computer type (`peer`), its address
  (`host`), the shared security key (`key`, with Generate and Copy), this
  computer's name (`name`), and the other screen's size (`remote_width`,
  `remote_height`).
- **Sharing.** Choose whether this computer controls the other one
  (`bidirectional`), whether to share the clipboard (`clipboard`) and whether
  to copy files (`file_transfer`).
- **Background service.** Start, stop or restart the service, choose whether
  it starts at login, and read the recent log.
- **Live status.** Read from `$XDG_RUNTIME_DIR/mwb/status.json`, which the
  client writes.
- **Tray icon.** Closing the window leaves an icon in the tray: click it to
  open the window, right-click for the connection state, Start/Stop sharing
  and Quit (which also stops the service). `mwb-gui --background` starts with
  just the icon; the autostart entry
  `io.github.lucky_verma.MwbLinux.Tray.desktop` runs it at login. The icon is
  a StatusNotifierItem published with Gio, since GTK 4 has no tray API; with
  no tray host on the desktop, closing the window quits.

## How it treats your config

Only the settings you change are written. Everything else in the file is
kept exactly as it was: comments, other settings such as
`accel_multiplier` or `legacy_crypto`, and any tables. The file is replaced
atomically and stays readable only by you (0600), because it holds the
security key.

If the file can't be parsed safely, the window refuses to save and leaves
the file untouched.

If the service runs `mwb` with `-bidi` or `-edge` (the Debian package's unit
does), those flags override the config. The window points this out.

## Tests

```bash
make test-gui
```

These tests need no GTK. The window was also checked under Xvfb in light and
dark themes.
