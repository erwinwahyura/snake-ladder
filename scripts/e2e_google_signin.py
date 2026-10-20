# Google sign-in against scripts/fake_google.py: a phone guest signs in and
# keeps its identity, a laptop signing in to the same account gets it too,
# and signing out gives a fresh guest.
# Usage: python3 scripts/e2e_google_signin.py http://127.0.0.1:8080 ./shots
import sys
from playwright.sync_api import sync_playwright
S = sys.argv[2] if len(sys.argv) > 2 else "."
B = sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8080"
errs = []
def page(b, tag, phone=False):
    kw = dict(viewport={"width": 390, "height": 844}, is_mobile=True, has_touch=True, device_scale_factor=2) if phone else dict(viewport={"width": 1100, "height": 760})
    p = b.new_context(bypass_csp=True, **kw).new_page()
    p.on("pageerror", lambda e: errs.append((tag, str(e))))
    p.on("dialog", lambda d: d.accept())
    return p
with sync_playwright() as pw:
    b = pw.chromium.launch(args=["--use-gl=angle", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"])
    # Phone: play a game against bots first so the guest has a record, then sign in.
    ph = page(b, "phone", phone=True)
    ph.goto(B, wait_until="domcontentloaded")
    ph.wait_for_selector("#google-in:not([hidden])", timeout=10000)
    ph.screenshot(path=f"{S}/32-lobby-google-mobile.png")
    guest = ph.evaluate("localStorage.getItem('ut.pid')")
    ph.click("#google-in")
    ph.wait_for_selector("#lobby-note:not([hidden])", timeout=15000)
    print("phone note:", ph.text_content("#lobby-note"))
    print("name prefilled:", ph.input_value("#name"))
    print("same identity kept:", ph.evaluate("localStorage.getItem('ut.pid')") == guest)
    print("status shown:", ph.is_visible("#google-status"), "| url clean:", "login" not in ph.url)
    ph.screenshot(path=f"{S}/33-signed-in-mobile.png")
    # Laptop: a fresh browser signs in with the same Google account.
    lp = page(b, "laptop")
    lp.goto(B, wait_until="domcontentloaded")
    lp.wait_for_selector("#google-in:not([hidden])", timeout=10000)
    lp.click("#google-in")
    lp.wait_for_selector("#lobby-note:not([hidden])", timeout=15000)
    print("laptop gets the phone's identity:", lp.evaluate("localStorage.getItem('ut.pid')") == guest)
    # Sign out on the laptop: back to a new guest.
    lp.click("#google-out")
    lp.wait_for_selector("#google-in:not([hidden])", timeout=10000)
    print("after sign-out, new guest:", lp.evaluate("localStorage.getItem('ut.pid')") != guest)
    b.close()
print("errors", errs)
