# Two people press "Cari lawan", land at the same table, and after the 30 s
# wait bots fill the empty seats and the game starts.
# Usage: python3 scripts/e2e_quick_match.py http://127.0.0.1:8080 ./shots
import sys
from playwright.sync_api import sync_playwright
S=sys.argv[2] if len(sys.argv) > 2 else "."
B=sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8080"
errs=[]
def page(b, tag, phone=False):
    kw=dict(viewport={"width":390,"height":844},is_mobile=True,has_touch=True,device_scale_factor=2) if phone else dict(viewport={"width":1100,"height":760})
    p=b.new_context(bypass_csp=True, **kw).new_page()
    p.on("pageerror", lambda e: errs.append((tag,str(e))))
    return p
with sync_playwright() as pw:
    b=pw.chromium.launch(args=["--use-gl=angle","--use-angle=swiftshader","--enable-unsafe-swiftshader"])
    a=page(b,"A",phone=True); g=page(b,"B")
    a.goto(B, wait_until="domcontentloaded"); a.wait_for_function("localStorage.getItem('ut.pid')")
    g.goto(B, wait_until="domcontentloaded"); g.wait_for_function("localStorage.getItem('ut.pid')")
    a.screenshot(path=f"{S}/28-lobby-cari-lawan.png")
    a.fill("#name","Budi"); a.click("#quick-match")
    a.wait_for_selector("#room-code:not(:empty)")
    print("A hint:", a.text_content("#waiting-hint"), flush=True)
    g.click("#refresh-rooms"); g.wait_for_selector("#room-list li", timeout=10000)
    print("B sees:", g.inner_text("#room-list").replace("\n"," | "))
    g.fill("#name","Sari"); g.click("#quick-match")
    g.wait_for_selector("#room-code:not(:empty)")
    a.screenshot(path=f"{S}/29-searching-mobile.png")
    print("same table:", a.text_content("#room-code")==g.text_content("#room-code"))
    a.wait_for_function("document.getElementById('waiting').hidden", timeout=45000)
    a.wait_for_timeout(1500)
    print("A players:", a.inner_text("#players").replace("\n"," | "))
    a.screenshot(path=f"{S}/30-match-started-mobile.png")
    b.close()
print("errors", errs)
