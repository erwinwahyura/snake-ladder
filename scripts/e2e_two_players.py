# Two players (desktop + phone) play real rolls against a running server.
# Usage: python3 scripts/e2e_two_players.py http://127.0.0.1:8080 ./shots 10
# Needs: pip install playwright && playwright install chromium
import sys, time
from playwright.sync_api import sync_playwright

BASE = sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8099"
OUT = sys.argv[2] if len(sys.argv) > 2 else "."
ROLLS = int(sys.argv[3]) if len(sys.argv) > 3 else 8
errors = []

def watch(page, tag):
    page.on("console", lambda m: m.type in ("error", "warning") and errors.append(f"[{tag}] {m.type}: {m.text}"))
    page.on("pageerror", lambda e: errors.append(f"[{tag}] pageerror: {e}"))

with sync_playwright() as p:
    b = p.chromium.launch(args=["--use-gl=angle", "--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--ignore-gpu-blocklist"])
    host = b.new_context(viewport={"width": 1280, "height": 800}, bypass_csp=True).new_page()
    guest = b.new_context(viewport={"width": 390, "height": 844}, is_mobile=True, has_touch=True, device_scale_factor=2, bypass_csp=True).new_page()
    watch(host, "host"); watch(guest, "guest")

    host.goto(BASE); host.wait_for_timeout(1500)
    host.screenshot(path=f"{OUT}/01-lobby.png")
    host.fill("#name", "Budi"); host.click("#create")
    host.wait_for_selector("#room-code:not(:empty)")
    code = host.text_content("#room-code"); print("room", code)

    guest.goto(f"{BASE}/?meja={code}"); guest.wait_for_timeout(800)
    guest.screenshot(path=f"{OUT}/02-guest-lobby-mobile.png")
    guest.fill("#name", "Sari"); guest.click("#join-form button")
    guest.wait_for_selector("#room:not([hidden])", state="attached")
    host.wait_for_timeout(800)
    host.check("input[value=bounce]"); host.wait_for_timeout(300)
    host.screenshot(path=f"{OUT}/03-host-waiting.png")
    guest.screenshot(path=f"{OUT}/04-guest-waiting-mobile.png")
    host.click("#start"); host.wait_for_timeout(1000)
    host.screenshot(path=f"{OUT}/05-host-start.png")
    guest.screenshot(path=f"{OUT}/06-guest-start-mobile.png")

    guest.click("#reactions button:nth-child(6)", force=True); host.wait_for_timeout(500)
    host.screenshot(path=f"{OUT}/12-reaction-host.png")
    guest.screenshot(path=f"{OUT}/13-reaction-guest-mobile.png")
    print("host log after reaction:", host.text_content("#log"))
    done = 0
    t0 = time.time()
    while done < ROLLS and time.time() - t0 < 120:
        for pg in (host, guest):
            if pg.is_enabled("#roll") and pg.is_visible("#roll"):
                try:
                    pg.click("#roll", force=True, timeout=1500); done += 1
                except Exception:
                    continue
                pg.wait_for_timeout(300)
                if done == 1:
                    pg.screenshot(path=f"{OUT}/07-mid-roll.png")
        host.wait_for_timeout(200)
        if host.evaluate("document.getElementById('results').open"):
            break
    host.wait_for_timeout(4000)
    host.screenshot(path=f"{OUT}/08-host-after.png")
    guest.screenshot(path=f"{OUT}/09-guest-after-mobile.png")
    print("rolls", done, "log:", host.text_content("#log"))
    print("players:", host.inner_text("#players").replace("\n", " | "))
    b.close()

print("ERRORS:" if errors else "no console errors")
for e in errors: print(e)
