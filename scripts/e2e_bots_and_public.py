# "Main lawan bot" on a phone, then a public 3-seat table found and joined from Meja terbuka.
# Usage: python3 scripts/e2e_bots_and_public.py http://127.0.0.1:8080 ./shots
from playwright.sync_api import sync_playwright
import sys
S=sys.argv[2] if len(sys.argv) > 2 else "."
B=sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8080"
errs=[]
with sync_playwright() as p:
    b=p.chromium.launch(args=["--use-gl=angle","--use-angle=swiftshader","--enable-unsafe-swiftshader"])
    # 1. quick game vs bots on a phone
    ph=b.new_context(viewport={"width":390,"height":844},is_mobile=True,has_touch=True,device_scale_factor=2,bypass_csp=True).new_page()
    ph.on("pageerror", lambda e: errs.append(("phone",str(e))))
    ph.goto(B); ph.wait_for_timeout(1200)
    print("pid", ph.evaluate("localStorage.getItem('ut.pid')"))
    ph.fill("#name","Budi"); ph.click("#vs-bots")
    ph.wait_for_function("window.document.querySelectorAll('#players li').length==4", timeout=15000)
    ph.wait_for_timeout(1500)
    ph.screenshot(path=f"{S}/20-vs-bots-mobile.png")
    print("players:", ph.inner_text("#players").replace("\n"," | "))
    ph.context.close()
    # 2. desktop host makes a public 3-seat table; another browser sees it in the list
    h=b.new_context(viewport={"width":1280,"height":800},bypass_csp=True).new_page()
    h.on("pageerror", lambda e: errs.append(("host",str(e))))
    h.goto(B, wait_until="domcontentloaded"); h.fill("#name","Sari"); h.click("#create")
    h.wait_for_selector("#room-code:not(:empty)")
    h.click("text=3 pemain"); h.check("input[value=public]"); h.click("#add-bot")
    h.wait_for_timeout(800)
    h.screenshot(path=f"{S}/21-host-setup.png")
    g=b.new_context(viewport={"width":1280,"height":800},bypass_csp=True).new_page()
    g.on("pageerror", lambda e: errs.append(("guest",str(e))))
    g.goto(B, wait_until="domcontentloaded"); g.wait_for_selector("#room-list li", timeout=10000)
    g.screenshot(path=f"{S}/22-open-tables.png")
    print("listed:", g.inner_text("#room-list").replace("\n"," | "))
    g.fill("#name","Joko"); g.click("#room-list button")
    h.wait_for_function("[...document.querySelectorAll('#players li .who')].some(e=>e.textContent.startsWith('Joko'))", timeout=10000)
    print("host sees:", h.inner_text("#players").replace("\n"," | "))
    h.screenshot(path=f"{S}/23-host-after-join.png")
    b.close()
print("errors", errs)
