"""GTK 4 settings and status window for mwb-linux.

Works with GTK 4.6 (Ubuntu 22.04) and newer, using only GTK itself so the
window takes the desktop's own theme, including dark mode.
"""

from __future__ import annotations

import math
import os
import secrets
import socket
import sys
import threading
import time

import gi

gi.require_version("Gtk", "4.0")
gi.require_version("Gdk", "4.0")
gi.require_version("PangoCairo", "1.0")
from gi.repository import Gdk, Gio, GLib, Gtk, Pango, PangoCairo  # noqa: E402

from . import __version__  # noqa: E402
from . import config_file as cf  # noqa: E402
from . import service  # noqa: E402
from . import tray  # noqa: E402

APP_ID = "io.github.lucky_verma.MwbLinux"

# Matches internal/config/keygen.go so keys look the same from either tool.
KEY_ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
KEY_LENGTH = 24

PEERS = [
    ("windows", "Windows with PowerToys"),
    ("linux", "Linux with mwb-linux"),
]
PEER_HELP = {
    "windows": "Get the security key from PowerToys → Mouse Without Borders on the Windows PC, "
    "and turn off “Move mouse relatively” there.",
    "linux": "Set the other computer to Linux too, with the same security key "
    "and a different computer name.",
}

STATUS_POLL_SECONDS = 2


def generate_key() -> str:
    return "".join(secrets.choice(KEY_ALPHABET) for _ in range(KEY_LENGTH))


def _theme_colors(widget: Gtk.Widget) -> tuple[Gdk.RGBA, Gdk.RGBA]:
    """Foreground and accent colours from the current theme."""
    if hasattr(widget, "get_color"):  # GTK 4.10+
        fg = widget.get_color()
    else:
        fg = widget.get_style_context().get_color()
    ctx = widget.get_style_context()
    for name in ("accent_bg_color", "accent_color", "theme_selected_bg_color"):
        try:
            found, rgba = ctx.lookup_color(name)
        except (AttributeError, TypeError):
            found = False
        if found:
            return fg, rgba
    accent = Gdk.RGBA()
    accent.parse("#3584e4")
    return fg, accent


def _rounded_rect(cr, x, y, w, h, r):
    r = min(r, w / 2, h / 2)
    cr.new_sub_path()
    cr.arc(x + w - r, y + r, r, -math.pi / 2, 0)
    cr.arc(x + w - r, y + h - r, r, 0, math.pi / 2)
    cr.arc(x + r, y + h - r, r, math.pi / 2, math.pi)
    cr.arc(x + r, y + r, r, math.pi, 3 * math.pi / 2)
    cr.close_path()


class ArrangementView(Gtk.DrawingArea):
    """Two screens side by side; click to swap them, or use the arrow keys.

    The screens are drawn in their real proportions and touch along the edge
    the cursor crosses, which is the one thing the user needs to get right.
    """

    def __init__(self, on_edge_changed):
        super().__init__()
        self.edge = "left"
        self.local_name = ""
        self.remote_name = ""
        self.remote_kind = "Windows PC"
        self.local_aspect = 16 / 9
        self.remote_aspect = 16 / 9
        self.connected = False
        self._on_edge_changed = on_edge_changed

        self.set_content_height(176)
        self.set_hexpand(True)
        self.set_focusable(True)
        self.set_draw_func(self._draw)

        click = Gtk.GestureClick()
        click.connect("pressed", self._clicked)
        self.add_controller(click)
        keys = Gtk.EventControllerKey()
        keys.connect("key-pressed", self._key)
        self.add_controller(keys)
        focus = Gtk.EventControllerFocus()
        focus.connect("enter", lambda *_: self.queue_draw())
        focus.connect("leave", lambda *_: self.queue_draw())
        self.add_controller(focus)
        self._update_accessible()

    def set_edge(self, edge: str, notify: bool = False):
        if edge not in ("left", "right") or edge == self.edge:
            return
        self.edge = edge
        self._update_accessible()
        self.queue_draw()
        if notify:
            self._on_edge_changed(edge)

    def refresh(self):
        self._update_accessible()
        self.queue_draw()

    def _update_accessible(self):
        text = f"Screen arrangement: the other computer is on the {self.edge}. Press Left or Right to move it."
        self.set_tooltip_text("Click to swap the two screens")
        try:
            self.update_property([Gtk.AccessibleProperty.LABEL], [text])
        except (AttributeError, TypeError):
            pass

    def _clicked(self, _gesture, _n, _x, _y):
        # Swap rather than "move the other computer to the clicked half": with
        # the halves fully covered by the two screens, clicking this computer
        # to say "I am here" used to put the other computer there instead.
        self.grab_focus()
        self.set_edge("right" if self.edge == "left" else "left", notify=True)

    def _key(self, _ctl, keyval, _code, _state):
        if keyval in (Gdk.KEY_Left, Gdk.KEY_KP_Left):
            self.set_edge("left", notify=True)
            return True
        if keyval in (Gdk.KEY_Right, Gdk.KEY_KP_Right):
            self.set_edge("right", notify=True)
            return True
        return False

    def _text(self, cr, text, x, y, fg, alpha, bold=False, align_center=True, max_w=None):
        layout = PangoCairo.create_layout(cr)
        desc = self.get_pango_context().get_font_description().copy()
        if bold:
            desc.set_weight(Pango.Weight.BOLD)
        layout.set_font_description(desc)
        layout.set_text(text, -1)
        if max_w:
            layout.set_width(int(max_w * Pango.SCALE))
            layout.set_ellipsize(Pango.EllipsizeMode.END)
            layout.set_alignment(Pango.Alignment.CENTER)
        w, h = layout.get_pixel_size()
        if max_w:
            x -= max_w / 2
        elif align_center:
            x -= w / 2
        cr.set_source_rgba(fg.red, fg.green, fg.blue, alpha)
        cr.move_to(x, y)
        PangoCairo.show_layout(cr, layout)
        return h

    def _draw(self, _area, cr, width, height):
        fg, accent = _theme_colors(self)
        label_h = 44
        max_h = height - label_h - 16
        # Same height for both, widths from each screen's aspect ratio.
        lh = rh = max_h
        lw = lh * self.local_aspect
        rw = rh * self.remote_aspect
        scale = min(1.0, (width - 32) / (lw + rw))
        lw, lh, rw, rh = lw * scale, lh * scale, rw * scale, rh * scale
        total = lw + rw
        x0 = (width - total) / 2
        top = 8
        # Bottom-align the screens, like monitors standing on a desk.
        base = top + max(lh, rh)

        if self.edge == "left":
            remote_x, local_x = x0, x0 + rw
            seam_x = local_x
        else:
            local_x, remote_x = x0, x0 + lw
            seam_x = remote_x

        def screen(x, w, h, fill, fill_a, stroke, stroke_a):
            _rounded_rect(cr, x + 1, base - h, w - 2, h, 8)
            cr.set_source_rgba(fill.red, fill.green, fill.blue, fill_a)
            cr.fill_preserve()
            cr.set_source_rgba(stroke.red, stroke.green, stroke.blue, stroke_a)
            cr.set_line_width(1.5)
            cr.stroke()

        screen(remote_x, rw, rh, fg, 0.05, fg, 0.35)
        screen(local_x, lw, lh, accent, 0.16, accent, 0.9)

        # The shared edge: where the cursor crosses.
        seam_h = min(lh, rh)
        cr.set_source_rgba(accent.red, accent.green, accent.blue, 1.0)
        cr.set_line_width(4)
        cr.set_line_cap(1)  # round
        cr.move_to(seam_x, base - seam_h + 10)
        cr.line_to(seam_x, base - 10)
        cr.stroke()
        if self.connected:
            # A small filled marker on the seam shows the link is live.
            cr.arc(seam_x, base - seam_h / 2, 6, 0, 2 * math.pi)
            cr.fill()

        ty = base + 6
        self._text(cr, self.local_name or "This computer", local_x + lw / 2, ty, fg, 0.95, bold=True, max_w=lw - 8)
        self._text(cr, "This computer", local_x + lw / 2, ty + 20, fg, 0.6, max_w=lw - 8)
        self._text(cr, self.remote_name or "Other computer", remote_x + rw / 2, ty, fg, 0.95, bold=True, max_w=rw - 8)
        self._text(cr, self.remote_kind, remote_x + rw / 2, ty + 20, fg, 0.6, max_w=rw - 8)

        if self.has_focus():
            _rounded_rect(cr, 2, 2, width - 4, height - 4, 10)
            cr.set_source_rgba(accent.red, accent.green, accent.blue, 0.7)
            cr.set_line_width(2)
            cr.stroke()


class StatusDot(Gtk.DrawingArea):
    COLORS = {
        "ok": "#2ec27e",
        "wait": "#e5a50a",
        "off": None,  # theme foreground, dimmed
        "error": "#e01b24",
    }

    def __init__(self):
        super().__init__()
        self.kind = "off"
        self.set_content_width(12)
        self.set_content_height(12)
        self.set_valign(Gtk.Align.CENTER)
        self.set_draw_func(self._draw)

    def set_kind(self, kind):
        if kind != self.kind:
            self.kind = kind
            self.queue_draw()

    def _draw(self, _a, cr, w, h):
        hexcolor = self.COLORS[self.kind]
        if hexcolor is None:
            fg, _ = _theme_colors(self)
            cr.set_source_rgba(fg.red, fg.green, fg.blue, 0.35)
        else:
            c = Gdk.RGBA()
            c.parse(hexcolor)
            cr.set_source_rgba(c.red, c.green, c.blue, 1)
        cr.arc(w / 2, h / 2, min(w, h) / 2, 0, 2 * math.pi)
        cr.fill()


def _section(title: str) -> tuple[Gtk.Box, Gtk.Grid]:
    box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=8)
    label = Gtk.Label(xalign=0)
    label.set_markup(f"<b>{GLib.markup_escape_text(title)}</b>")
    box.append(label)
    frame = Gtk.Frame()
    grid = Gtk.Grid(column_spacing=16, row_spacing=10)
    for side in ("start", "end", "top", "bottom"):
        getattr(grid, f"set_margin_{side}")(14)
    frame.set_child(grid)
    box.append(frame)
    return box, grid


def _dim(text: str, wrap=True) -> Gtk.Label:
    lab = Gtk.Label(label=text, xalign=0, wrap=wrap)
    lab.add_css_class("dim-label")
    lab.set_wrap_mode(Pango.WrapMode.WORD_CHAR)
    lab.set_max_width_chars(60)
    return lab


class MainWindow(Gtk.ApplicationWindow):
    def __init__(self, app, config_path: str):
        super().__init__(application=app, title="Mouse Without Borders")
        self.config_path = config_path
        self.set_default_size(600, 860)
        self._loading = False
        self._loaded: dict[str, object] = {}
        self._unit = service.UnitState()
        self._poll_busy = False
        self._connected_remote = ""
        self._peer_screen = None  # (w, h) a connected Linux peer reported
        self._status_addresses: list[str] = []  # this machine's, as mwb reported them

        scroller = Gtk.ScrolledWindow(hscrollbar_policy=Gtk.PolicyType.NEVER)
        outer = Gtk.Box(orientation=Gtk.Orientation.VERTICAL)
        scroller.set_child(outer)
        scroller.set_vexpand(True)

        page = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=22)
        for side in ("start", "end", "top", "bottom"):
            getattr(page, f"set_margin_{side}")(20)
        outer.append(page)

        page.append(self._build_status())
        self.arrangement = ArrangementView(self._edge_changed)
        arr_box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=6)
        arr_box.append(self.arrangement)
        self.arrangement_hint = _dim("")
        self.arrangement_hint.set_justify(Gtk.Justification.CENTER)
        self.arrangement_hint.set_xalign(0.5)
        arr_box.append(self.arrangement_hint)
        page.append(arr_box)
        page.append(self._build_connection())
        page.append(self._build_sharing())
        page.append(self._build_service())

        root = Gtk.Box(orientation=Gtk.Orientation.VERTICAL)
        root.append(scroller)
        root.append(Gtk.Separator())
        root.append(self._build_actions())
        self.set_child(root)

        self.reload()
        self._poll()
        self._poll_source = GLib.timeout_add_seconds(STATUS_POLL_SECONDS, self._poll)
        self.connect("close-request", self._on_close)
        settings = Gtk.Settings.get_default()
        if settings is not None:
            for prop in ("gtk-theme-name", "gtk-application-prefer-dark-theme"):
                settings.connect(f"notify::{prop}", lambda *_: self._redraw_art())

    # --- building ---------------------------------------------------------

    def _build_status(self) -> Gtk.Widget:
        row = Gtk.Box(spacing=12)
        self.dot = StatusDot()
        row.append(self.dot)
        text = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=2, hexpand=True)
        self.status_title = Gtk.Label(xalign=0, wrap=True)
        self.status_title.set_markup("<b>Checking…</b>")
        self.status_detail = _dim("")
        text.append(self.status_title)
        text.append(self.status_detail)
        row.append(text)
        self.btn_start = Gtk.Button(label="Start")
        self.btn_start.connect("clicked", lambda *_: self._service("start"))
        self.btn_restart = Gtk.Button(label="Restart")
        self.btn_restart.connect("clicked", lambda *_: self._service("restart"))
        self.btn_stop = Gtk.Button(label="Stop")
        self.btn_stop.connect("clicked", lambda *_: self._service("stop"))
        for b in (self.btn_start, self.btn_restart, self.btn_stop):
            b.set_valign(Gtk.Align.CENTER)
            row.append(b)
        return row

    def _field(self, grid, row, label_text, widget, help_text=None, label_for=None):
        lab = Gtk.Label(label=label_text, xalign=0)
        # Also gives screen readers the label for the control.
        lab.set_mnemonic_widget(label_for or widget)
        lab.set_valign(Gtk.Align.START)
        lab.set_margin_top(6)
        grid.attach(lab, 0, row, 1, 1)
        col = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=4, hexpand=True)
        col.append(widget)
        hint = _dim(help_text or "")
        hint.set_visible(bool(help_text))
        col.append(hint)
        err = Gtk.Label(xalign=0, wrap=True)
        err.add_css_class("error")
        err.set_visible(False)
        col.append(err)
        grid.attach(col, 1, row, 1, 1)
        return hint, err

    def _build_connection(self) -> Gtk.Widget:
        box, grid = _section("Connection")
        self.peer = Gtk.DropDown.new_from_strings([label for _, label in PEERS])
        self.peer.connect("notify::selected", self._changed)
        self.peer_hint, self.peer_err = self._field(grid, 0, "Other computer", self.peer, PEER_HELP["windows"])

        self.host = Gtk.Entry(placeholder_text="192.168.1.100 or desk.local", hexpand=True)
        self.host.connect("changed", self._changed)
        self.host_hint, self.host_err = self._field(grid, 1, "Address", self.host)
        self._local_address = service.local_address()

        key_row = Gtk.Box(spacing=6)
        self.key = Gtk.PasswordEntry(hexpand=True)
        self.key.set_show_peek_icon(True)
        self.key.connect("changed", self._changed)
        gen = Gtk.Button(label="Generate")
        gen.set_tooltip_text("Make a new random key. Enter the same key on the other computer.")
        gen.connect("clicked", self._generate_key)
        copy = Gtk.Button(label="Copy")
        copy.connect("clicked", self._copy_key)
        key_row.append(self.key)
        key_row.append(gen)
        key_row.append(copy)
        self.key_hint, self.key_err = self._field(grid, 2, "Security key", key_row,
                                                  "Both computers must use the same key.", label_for=self.key)

        self.name = Gtk.Entry(placeholder_text=socket.gethostname()[: cf.NAME_MAX], max_length=cf.NAME_MAX)
        self.name.connect("changed", self._changed)
        self.name_hint, self.name_err = self._field(grid, 3, "This computer’s name", self.name)

        size_row = Gtk.Box(spacing=6)
        self.rw = Gtk.SpinButton.new_with_range(200, 16384, 1)
        self.rh = Gtk.SpinButton.new_with_range(200, 16384, 1)
        for s in (self.rw, self.rh):
            s.set_numeric(True)
            s.connect("value-changed", self._changed)
        size_row.append(self.rw)
        size_row.append(Gtk.Label(label="×"))
        size_row.append(self.rh)
        size_row.append(_dim("pixels", wrap=False))
        self.size_hint, self.size_err = self._field(grid, 4, "Other screen size", size_row,
                                       "Used to place the pointer on the other screen.", label_for=self.rw)
        return box

    def _switch_row(self, grid, row, title, subtitle):
        text = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=2, hexpand=True)
        text.append(Gtk.Label(label=title, xalign=0, wrap=True))
        sub = _dim(subtitle)
        text.append(sub)
        sw = Gtk.Switch(valign=Gtk.Align.CENTER)
        grid.attach(text, 0, row, 1, 1)
        grid.attach(sw, 1, row, 1, 1)
        try:
            sw.update_property([Gtk.AccessibleProperty.LABEL], [title])
        except (AttributeError, TypeError):
            pass
        return sw, sub

    def _build_sharing(self) -> Gtk.Widget:
        box, grid = _section("Sharing")
        self.bidi, self.bidi_sub = self._switch_row(
            grid, 0, "Control the other computer from here",
            "Move the pointer past the shared edge to use this keyboard and mouse there. "
            "When off, the other computer can still control this one.")
        self.clip, _ = self._switch_row(grid, 1, "Share the clipboard", "Copied text and images are available on both computers.")
        self.files, self.files_sub = self._switch_row(
            grid, 2, "Copy files", "Copy one file, then paste it on the other computer. Up to 100 MB.")
        for sw in (self.bidi, self.clip, self.files):
            sw.connect("notify::active", self._changed)
        self.bidi.connect("notify::active", self._bidi_toggled)
        return box

    def _build_service(self) -> Gtk.Widget:
        box, grid = _section("Background service")
        self.autostart, self.autostart_sub = self._switch_row(
            grid, 0, "Start when I log in", "Runs mwb as a user service, without administrator rights.")
        self.autostart.connect("state-set", self._autostart_set)
        self.override_note = Gtk.Label(xalign=0, wrap=True)
        self.override_note.add_css_class("warning")
        self.override_note.set_visible(False)
        grid.attach(self.override_note, 0, 1, 2, 1)

        self.log_expander = Gtk.Expander(label="Recent log")
        self.log_view = Gtk.TextView(editable=False, monospace=True, cursor_visible=False,
                                     wrap_mode=Gtk.WrapMode.WORD_CHAR)
        self.log_view.set_size_request(-1, 160)
        log_scroll = Gtk.ScrolledWindow(min_content_height=160)
        log_scroll.set_child(self.log_view)
        self.log_expander.set_child(log_scroll)
        self.log_expander.connect("notify::expanded", self._log_expanded)
        grid.attach(self.log_expander, 0, 2, 2, 1)
        return box

    def _build_actions(self) -> Gtk.Widget:
        bar = Gtk.Box(spacing=8)
        for side in ("start", "end", "top", "bottom"):
            getattr(bar, f"set_margin_{side}")(12)
        self.save_msg = Gtk.Label(xalign=0, hexpand=True, wrap=True)
        self.save_msg.add_css_class("dim-label")
        bar.append(self.save_msg)
        self.btn_apply_restart = Gtk.Button(label="Restart now")
        self.btn_apply_restart.set_visible(False)
        self.btn_apply_restart.connect("clicked", lambda *_: self._service("restart"))
        bar.append(self.btn_apply_restart)
        self.btn_revert = Gtk.Button(label="Revert")
        self.btn_revert.connect("clicked", lambda *_: self.reload())
        bar.append(self.btn_revert)
        self.btn_save = Gtk.Button(label="Save")
        self.btn_save.add_css_class("suggested-action")
        self.btn_save.connect("clicked", self._save)
        bar.append(self.btn_save)
        return bar

    # --- form state --------------------------------------------------------

    def reload(self):
        self._loading = True
        doc = None
        try:
            try:
                doc = cf.load(self.config_path)
                values = doc.values()
                self.save_msg.set_text("" if doc.lines else "No settings yet. Fill in the connection, then save.")
                self._config_error = ""
            except (cf.ConfigError, OSError) as exc:
                values = dict(cf.DEFAULTS)
                self._config_error = f"The settings file can’t be edited here: {exc}. Fix {self.config_path} by hand."
                self.save_msg.set_text(self._config_error)
            self._loaded = values
            # Unset in the file means "the default for this peer type", which
            # should follow the peer choice until the user flips the switch.
            self._bidi_follows_peer = doc is None or "bidirectional" not in doc.entries
            self._last_peer = values["peer"]
            self.peer.set_selected(0 if values["peer"] != "linux" else 1)
            self.host.set_text(str(values["host"]))
            self.key.set_text(str(values["key"]))
            self.name.set_text(str(values["name"]))
            self.rw.set_value(int(values["remote_width"]))
            self.rh.set_value(int(values["remote_height"]))
            self.bidi.set_active(bool(values["bidirectional"]))
            self.clip.set_active(bool(values["clipboard"]))
            self.files.set_active(bool(values["file_transfer"]))
            self.arrangement.set_edge(str(values["edge"]))
            self.btn_apply_restart.set_visible(False)
        finally:
            self._loading = False
        self._changed()

    def form_values(self) -> dict[str, object]:
        return {
            "peer": PEERS[self.peer.get_selected()][0] if self.peer.get_selected() < len(PEERS) else "windows",
            "host": self.host.get_text().strip(),
            "key": self.key.get_text(),
            "name": self.name.get_text().strip(),
            "edge": self.arrangement.edge,
            "bidirectional": self.bidi.get_active(),
            "clipboard": self.clip.get_active(),
            "file_transfer": self.files.get_active(),
            "remote_width": int(self.rw.get_value()),
            "remote_height": int(self.rh.get_value()),
        }

    def changed_values(self) -> dict[str, object]:
        """Only the settings the user changed, so a save never overwrites an
        edit someone made to another key in the file meanwhile."""
        return {k: v for k, v in self.form_values().items() if self._loaded.get(k) != v}

    def _edge_changed(self, _edge):
        self._update_arrangement_hint()
        self._changed()

    def _update_arrangement_hint(self):
        other = self.arrangement.remote_name or "The other computer"
        self.arrangement_hint.set_text(
            f"{other} is to the {self.arrangement.edge} of this computer. "
            "Click the screens to swap them. The highlighted edge is where the pointer crosses.")

    def _bidi_toggled(self, *_):
        if not self._loading:
            self._bidi_follows_peer = False

    def _follow_peer_default(self, peer: str):
        """Keep an unset bidirectional at its default when the peer changes."""
        if peer == getattr(self, "_last_peer", peer):
            return
        self._last_peer = peer
        if getattr(self, "_bidi_follows_peer", False):
            self._loading = True
            try:
                self.bidi.set_active(peer == "linux")
            finally:
                self._loading = False

    def _changed(self, *_):
        if self._loading:
            return
        self._follow_peer_default(PEERS[self.peer.get_selected()][0]
                                  if self.peer.get_selected() < len(PEERS) else "windows")
        v = self.form_values()
        peer = str(v["peer"])
        self.peer_hint.set_text(PEER_HELP[peer])
        self.arrangement.local_name = v["name"] or socket.gethostname()[: cf.NAME_MAX]
        # Once connected, the peer's own name is friendlier than its address.
        self.arrangement.remote_name = self._connected_remote or v["host"]
        self._update_arrangement_hint()
        self.arrangement.remote_kind = "Linux computer" if peer == "linux" else "Windows PC"
        if self._peer_screen:
            # The peer measured its own screen; mwb uses that, so draw it.
            pw, ph = self._peer_screen
            self.arrangement.remote_aspect = pw / ph
            self.size_hint.set_text(f"{self._connected_remote or 'The other computer'} reports {pw} × {ph}. "
                                    "mwb uses that automatically; this value is only a fallback.")
        else:
            if v["remote_height"]:
                self.arrangement.remote_aspect = int(v["remote_width"]) / int(v["remote_height"])
            self.size_hint.set_text("Used to place the pointer on the other screen.")
        self.arrangement.local_aspect = self._local_aspect()
        self.arrangement.refresh()
        self.files.set_sensitive(bool(v["clipboard"]))
        self.files_sub.set_text(
            "Copy one file, then paste it on the other computer. Up to 100 MB."
            if v["clipboard"] else "Needs clipboard sharing, which starts each copy.")
        # Like pairing PowerToys machines: one waits, the other connects.
        if peer == "linux":
            self.host.set_placeholder_text("Leave empty to wait for the other computer")
            here = self._local_address or (self._status_addresses[0] if self._status_addresses else "")
            where = (f"this computer’s address, {here}" if here
                     else "this computer’s address (run hostname -I to see it)")
            self.host_hint.set_text(
                f"Leave it empty on one computer, and on the other enter {where}."
                if v["host"] else
                f"This computer will wait. On the other computer, enter {where}.")
        else:
            self.host.set_placeholder_text("192.168.1.100 or desk.local")
            self.host_hint.set_text("")
        self.host_hint.set_visible(peer == "linux")
        self.name_hint.set_visible(peer == "linux")
        self.name_hint.set_text("Must be different from the other computer’s name.")
        self.bidi_sub.set_text("Move the pointer past the shared edge to use this keyboard and mouse there. "
                               "When off, the other computer can still control this one.")

        problems = cf.validate(v)
        advice = cf.warnings(v)
        for key, err in (("peer", self.peer_err), ("host", self.host_err), ("key", self.key_err),
                         ("name", self.name_err), ("remote_width", self.size_err)):
            msg = problems.get(key) or (problems.get("remote_height") if key == "remote_width" else None)
            soft = advice.get(key) if not msg else None
            err.set_text(msg or soft or "")
            err.set_visible(bool(msg or soft))
            # Advice reads as helper text; only blocking problems use the error style.
            (err.remove_css_class if soft else err.add_css_class)("error")
            (err.add_css_class if soft else err.remove_css_class)("dim-label")
        dirty = bool(self.changed_values()) or not os.path.exists(self.config_path)
        can_save = dirty and not problems and not getattr(self, "_config_error", "")
        self.btn_save.set_sensitive(can_save)
        self.btn_revert.set_sensitive(bool(self.changed_values()))
        if dirty and self.btn_apply_restart.get_visible():
            self.btn_apply_restart.set_visible(False)

    def _local_aspect(self) -> float:
        display = Gdk.Display.get_default()
        try:
            monitors = display.get_monitors()
            mon = monitors.get_item(0) if monitors.get_n_items() else None
            if mon is not None:
                g = mon.get_geometry()
                if g.height > 0:
                    return g.width / g.height
        except (AttributeError, TypeError):
            pass
        return 16 / 9

    def _redraw_art(self):
        self.arrangement.queue_draw()
        self.dot.queue_draw()

    # --- actions -----------------------------------------------------------

    def _generate_key(self, *_):
        self.key.set_text(generate_key())
        self.key.set_show_peek_icon(True)
        self.save_msg.set_text("New key generated. Enter the same key on the other computer, then save.")

    def _copy_key(self, *_):
        text = self.key.get_text()
        if not text:
            return
        clipboard = self.get_clipboard()
        try:
            clipboard.set(text)
        except TypeError:
            clipboard.set_content(Gdk.ContentProvider.new_for_value(text))
        self.save_msg.set_text("Security key copied.")

    def _save(self, *_):
        updates = self.changed_values()
        if not os.path.exists(self.config_path):
            # A brand-new file needs the required settings even if unchanged.
            fv = self.form_values()
            # bidirectional is left out: unset means the peer type's default.
            for k in ("host", "key", "peer", "edge"):
                updates.setdefault(k, fv[k])
        if not updates.get("name", True):
            updates.pop("name")  # empty means "use the hostname"; don't write ""
        if "edge" in updates:
            # With a Linux peer, the most recently chosen side wins and the
            # other computer saves the mirror of it.
            updates["edge_changed"] = int(time.time())
        try:
            cf.save(updates, self.config_path)
        except (cf.ConfigError, OSError) as exc:
            self.save_msg.set_text(f"Not saved: {exc}")
            return
        self.reload()
        if self._unit.running:
            self.save_msg.set_text("Saved. Restart the service to use the new settings.")
            self.btn_apply_restart.set_visible(True)
        else:
            self.save_msg.set_text("Saved.")

    def _service(self, action):
        self.save_msg.set_text({"start": "Starting…", "stop": "Stopping…", "restart": "Restarting…"}.get(action, ""))

        def work():
            err = service.control(action)
            GLib.idle_add(done, err)

        def done(err):
            if err:
                self.save_msg.set_text(f"Couldn’t {action} the service: {err}")
            else:
                self.save_msg.set_text({"start": "Started.", "stop": "Stopped.", "restart": "Restarted."}[action])
                self.btn_apply_restart.set_visible(False)
            self._poll()
            return False

        threading.Thread(target=work, daemon=True).start()

    def _autostart_set(self, _sw, state):
        if self._loading:
            return False

        def work():
            err = service.control("enable" if state else "disable")
            GLib.idle_add(done, err)

        def done(err):
            if err:
                self.save_msg.set_text(f"Couldn’t change start at login: {err}")
            self._poll()
            return False

        threading.Thread(target=work, daemon=True).start()
        return False

    def _log_expanded(self, exp, _pspec):
        if not exp.get_expanded():
            return

        def work():
            text = service.recent_log()
            GLib.idle_add(lambda: self.log_view.get_buffer().set_text(text) or False)

        threading.Thread(target=work, daemon=True).start()

    # --- status ------------------------------------------------------------

    def _poll(self):
        if self._poll_busy:
            return True
        self._poll_busy = True

        def work():
            unit = service.query()
            st = service.read_status()
            problem = service.uinput_problem()
            GLib.idle_add(self._show_status, unit, st, problem)

        threading.Thread(target=work, daemon=True).start()
        return True

    def _show_status(self, unit: service.UnitState, st: dict | None, problem=None):
        self._poll_busy = False
        self._unit = unit
        connected = False
        if st and st.get("state") == "connected":
            kind, title, detail = service.describe_connection(st)
            connected = True
        elif st and st.get("state") == "connecting" and st.get("waiting"):
            addrs = ", ".join(st.get("addresses") or []) or self._local_address or "this computer’s address"
            kind, title = "wait", "Waiting for the other computer"
            detail = f"On the other computer, enter {addrs} as the address, with the same security key."
        elif st and st.get("state") == "connecting":
            kind, title = "wait", f"Looking for {st.get('host')}…"
            detail = "Make sure the other computer is on and using the same security key."
        elif problem and not unit.running:
            # The exact cause and fix, ahead of the generic service states.
            kind, (title, detail) = "error", problem
        elif unit.crash_looping:
            kind, title = "error", "Keeps stopping"
            detail = "mwb exits with an error and systemd keeps restarting it. Open Recent log below to see why."
        elif unit.running:
            kind, title, detail = "ok", "Running", "Waiting for connection details from mwb."
        elif unit.error:
            kind, title = "off", "Can’t check the background service"
            detail = f"systemctl reported: {unit.error}"
        elif not unit.installed:
            kind, title = "off", "Not set up to run in the background"
            detail = "Install the mwb service, or run mwb from a terminal."
        elif unit.active == "failed":
            kind, title, detail = "error", "Stopped after an error", "Open Recent log below to see why."
        else:
            kind, title, detail = "off", "Not running", "Start mwb to connect to the other computer."
        self.dot.set_kind(kind)
        self.status_title.set_markup(f"<b>{GLib.markup_escape_text(title)}</b>")
        self.status_detail.set_text(detail)
        self.status_detail.set_visible(bool(detail))
        addresses = [str(a) for a in (st.get("addresses") or [])] if st else []
        if addresses != self._status_addresses:
            self._status_addresses = addresses
            self._changed()
        remote = str(st.get("remote") or "") if connected and st else ""
        pw = int(st.get("peer_screen_width") or 0) if connected and st else 0
        ph = int(st.get("peer_screen_height") or 0) if connected and st else 0
        peer_screen = (pw, ph) if pw > 0 and ph > 0 else None
        if (self.arrangement.connected != connected or self._connected_remote != remote
                or self._peer_screen != peer_screen):
            self.arrangement.connected = connected
            self._connected_remote = remote
            self._peer_screen = peer_screen
            self._changed()  # redraws the arrangement and the size hint

        # mwb saved the mirror of a side chosen more recently on the peer.
        adopted_from = str(st.get("edge_from") or "") if st else ""
        adopted = (adopted_from, st.get("edge")) if adopted_from and st else None
        if (adopted and adopted != getattr(self, "_adoption_seen", None) and adopted[1] in ("left", "right")
                and adopted[1] != self._loaded.get("edge") and "edge" not in self.changed_values()):
            self._adoption_seen = adopted
            self.reload()
            self.save_msg.set_text(f"Screen arrangement updated to match {adopted_from}, where it was changed more recently.")

        # A crash loop is still a started unit: offer Stop to end it.
        started = unit.running or unit.crash_looping
        self.btn_start.set_visible(unit.installed and not started)
        self.btn_stop.set_visible(unit.installed and started)
        self.btn_restart.set_visible(unit.installed and started)
        self.autostart.set_sensitive(unit.installed)
        self._loading = True
        self.autostart.set_active(unit.enabled)
        self._loading = False

        flags = unit.overriding_flags()
        if flags:
            self.override_note.set_text(
                "The service starts mwb with " + " ".join(flags) +
                ", which overrides the matching settings above. Remove those options from "
                "the service to manage them here.")
        self.override_note.set_visible(bool(flags))
        return False

    def _on_close(self, *_):
        if getattr(self, "_poll_source", None):
            GLib.source_remove(self._poll_source)
            self._poll_source = None
        return False


class MwbApp(Gtk.Application):
    def __init__(self, config_path: str, background: bool = False):
        super().__init__(application_id=APP_ID, flags=Gio.ApplicationFlags.DEFAULT_FLAGS
                         if hasattr(Gio.ApplicationFlags, "DEFAULT_FLAGS") else Gio.ApplicationFlags.FLAGS_NONE)
        self.config_path = config_path
        self.background = background  # started at login: only the tray icon
        self.tray = None

    def do_startup(self):
        Gtk.Application.do_startup(self)
        if not tray.available():
            return  # no tray host: closing the window quits, as before
        try:
            self.tray = tray.TrayIcon(self._tray_open, self._tray_toggle, self._tray_quit)
        except GLib.Error as exc:
            print(f"mwb-gui: no tray icon: {exc.message}", file=sys.stderr)
            return
        # The icon keeps the app alive with its window closed.
        self.hold()
        self._tray_poll()
        GLib.timeout_add_seconds(STATUS_POLL_SECONDS, self._tray_poll)

    def do_activate(self):
        if self.background:
            self.background = False  # later activations, from the icon or the launcher, open the window
            if self.tray is not None:
                return
        win = self.get_active_window()
        if win is None:
            win = MainWindow(self, self.config_path)
        win.present()

    def do_shutdown(self):
        if self.tray is not None:
            self.tray.close()
            self.tray = None
        Gtk.Application.do_shutdown(self)

    # --- tray -------------------------------------------------------------

    def _tray_poll(self):
        if self.tray is None:
            return False
        self.tray.update(*tray.tray_status(service.read_status()))
        return True

    def _tray_open(self):
        self.activate()

    def _tray_toggle(self, start: bool):
        def work():
            service.control("start" if start else "stop")
            GLib.idle_add(self._tray_poll)

        threading.Thread(target=work, daemon=True).start()

    def _tray_quit(self):
        # Quit means everything: stop sharing too, not just hide the icon.
        def work():
            service.control("stop")
            GLib.idle_add(self.quit)

        for win in self.get_windows():
            win.close()
        threading.Thread(target=work, daemon=True).start()


def main(argv=None) -> int:
    argv = list(sys.argv if argv is None else argv)
    config_path = cf.DEFAULT_PATH
    if "--version" in argv:
        print("mwb-gui", __version__)
        return 0
    background = "--background" in argv
    if background:
        argv.remove("--background")
    if "--config" in argv:
        i = argv.index("--config")
        if i + 1 >= len(argv):
            print("usage: mwb-gui [--config PATH] [--background]", file=sys.stderr)
            return 2
        config_path = os.path.abspath(os.path.expanduser(argv[i + 1]))
        del argv[i : i + 2]
    if background and not os.path.exists(config_path):
        return 0  # started at login, but mwb was never set up: no icon
    return MwbApp(config_path, background).run(argv[:1])
