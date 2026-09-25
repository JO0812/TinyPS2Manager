#!/usr/bin/env python3
"""Drive the real app UI in real WebKitGTK (the desktop engine) headless.

Runs under xvfb-run. Loads BASE in a shown Gtk window (so rAF ticks),
installs an injected script at document-start (fetch counter, rAF
heartbeat, window.onerror collector), then reproduces the Drive Options
override-dropdown change and reports PASS/FAIL per check.

Env: BASE=http://127.0.0.1:PORT
Exit nonzero on any failure. Any js() call that does not resolve within
its timeout is reported as a hang (main-thread freeze).
"""
import json
import os
import sys
import time

import gi

gi.require_version("Gtk", "3.0")
gi.require_version("WebKit2", "4.1")
from gi.repository import Gtk, WebKit2, GLib  # noqa: E402

BASE = os.environ.get("BASE")
if not BASE:
    print("BASE env required", file=sys.stderr)
    sys.exit(2)

INJECT = """
window.__fetchLog = [];
(function () {
  var origFetch = window.fetch;
  window.fetch = function () {
    try {
      var a = arguments[0];
      var url = typeof a === 'string' ? a : a.url;
      var m = (arguments[1] && arguments[1].method) || 'GET';
      window.__fetchLog.push(m + ' ' + url);
    } catch (e) {}
    return origFetch.apply(this, arguments);
  };
})();
window.__hb = 0;
(function tick() { window.__hb++; requestAnimationFrame(tick); })();
// rAF is throttled in offscreen/occluded WebKit views (xvfb has no
// compositor), so liveness is measured with a timer counter instead.
window.__hbTimer = 0;
setInterval(function () { window.__hbTimer++; }, 100);
window.__errs = [];
window.onerror = function (msg) { window.__errs.push(String(msg)); };
"""

results = []


def check(name, ok, extra=""):
    results.append(f"{'PASS' if ok else 'FAIL'} {name} {extra}")
    if not ok:
        global failed
        failed = True


failed = False


class Harness:
    def __init__(self):
        self.view = WebKit2.WebView()
        ucm = self.view.get_user_content_manager()
        us = WebKit2.UserScript.new(
            INJECT,
            WebKit2.UserContentInjectedFrames.ALL_FRAMES,
            WebKit2.UserScriptInjectionTime.START,
            None,
            None,
        )
        ucm.add_script(us)
        self.win = Gtk.Window()
        self.win.set_default_size(1600, 900)
        self.win.add(self.view)
        self.win.show_all()
        self._loaded = False
        self.view.connect("load-changed", self._on_load)

    def _on_load(self, view, event):
        if event == WebKit2.LoadEvent.FINISHED:
            self._loaded = True

    def pump_until(self, cond, timeout, what):
        """Pump the main context until cond() or timeout. Returns bool."""
        deadline = time.time() + timeout
        while time.time() < deadline:
            try:
                if cond():
                    return True
            except Exception:
                pass
            Gtk.main_iteration_do(False)
            time.sleep(0.01)
        try:
            return bool(cond())
        except Exception as e:
            print(f"TIMEOUT waiting for {what}: {e}", file=sys.stderr)
            return False

    def js(self, script, timeout=15, what="js"):
        """Run JS, return its stringified value; None on hang/error."""
        box = {}

        def cb(view, res, _):
            try:
                jsc = view.run_javascript_finish(res)
                try:
                    box["v"] = jsc.get_js_value().to_string()
                except Exception:
                    box["v"] = jsc.to_string()
            except Exception as e:  # noqa: BLE001
                box["e"] = str(e)

        wrapped = "JSON.stringify((function(){ " + script + " })())"
        self.view.run_javascript(wrapped, None, cb, None)
        if not self.pump_until(lambda: "v" in box or "e" in box, timeout, what):
            return None, "hang"
        if "e" in box:
            return None, box["e"]
        try:
            return json.loads(box["v"]), None
        except Exception:  # noqa: BLE001
            return box["v"], None


def main():
    h = Harness()
    h.view.load_uri(BASE + "/")
    if not h.pump_until(lambda: h._loaded, 30, "initial load"):
        check("page-load", False, "never finished")
        print("\n".join(results))
        return
    time.sleep(1.0)

    v, err = h.js("return document.querySelectorAll('.game-card').length", what="count cards")
    print(f"INFO cards={v} err={err}")

    # Go to Drive Options.
    h.js(
        "return [...document.querySelectorAll('aside nav button')]"
        ".find(x => (x.title||'').includes('Drive')).click(), 'clicked'"
    )
    time.sleep(1.5)
    v, err = h.js(
        "return document.querySelectorAll('.drives select').length + "
        "document.querySelectorAll('.detail select').length",
        what="drive view",
    )
    check("drive-view-renders", isinstance(v, int) and v >= 2, f"selects={v} {err or ''}")

    # Liveness baseline (timer counter; rAF throttles under xvfb).
    a, _ = h.js("return window.__hbTimer", what="hb0")
    time.sleep(0.5)
    b, _ = h.js("return window.__hbTimer", what="hb1")
    check("heartbeat-alive-before", a is not None and b is not None and b > a, f"hb {a}->{b}")

    # Change the override dropdown like a user.
    h.js(
        "var s=document.querySelector('.detail select');"
        " s.value='fat32'; s.dispatchEvent(new Event('change',{bubbles:true}));"
        " return s.value"
    )
    ok = h.pump_until(
        lambda: h.js("return document.querySelector('.detail select').value", timeout=5)[0]
        == "fat32",
        10,
        "select keeps fat32",
    )
    check("select-keeps-value", ok)
    time.sleep(3.0)

    c, err = h.js("return window.__hbTimer", what="hb2")
    time.sleep(0.5)
    d, err = h.js("return window.__hbTimer", what="hb3")
    check("heartbeat-alive-after", c is not None and d is not None and d > c, f"hb {c}->{d} {err or ''}")

    rows, _ = h.js(
        "return document.querySelectorAll('.preflight .check').length", what="preflight rows"
    )
    check("preflight-rendered", isinstance(rows, int) and rows > 0, f"rows={rows}")

    errs, _ = h.js("return window.__errs", what="window errors")
    check("no-window-errors", errs == [], json.dumps(errs)[:300] if errs else "")

    log, _ = h.js("return window.__fetchLog", what="fetch log")
    if isinstance(log, list):
        pf = sum(1 for l in log if "/preflight" in l)
        check("no-preflight-storm", pf <= 4, f"preflight={pf}")
    else:
        check("no-preflight-storm", False, f"log={log}")

    print("\n".join(results))


main()
sys.exit(1 if failed else 0)
