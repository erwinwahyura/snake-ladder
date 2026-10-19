# Two people play, become Teman main, see each other online; one invites the other
# from a new table; then a transfer code moves an identity to a third browser.
# Usage: python3 scripts/e2e_friends_invites.py http://127.0.0.1:8080 ./shots
import sys
from playwright.sync_api import sync_playwright
S=sys.argv[2] if len(sys.argv) > 2 else "."
B=sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8080"
errs=[]
def page(b, tag, phone=False):
    kw=dict(viewport={"width":390,"height":844},is_mobile=True,has_touch=True,device_scale_factor=2) if phone else dict(viewport={"width":1100,"height":760})
    ctx=b.new_context(bypass_csp=True, **kw); p=ctx.new_page()
    p.on("pageerror", lambda e: errs.append((tag,str(e))))
    p.on("dialog", lambda d: d.accept())
    return p
with sync_playwright() as pw:
    b=pw.chromium.launch(args=["--use-gl=angle","--use-angle=swiftshader","--enable-unsafe-swiftshader"])
    a=page(b,"A"); g=page(b,"B",phone=True)
    a.goto(B, wait_until="domcontentloaded"); g.goto(B, wait_until="domcontentloaded")
    a.wait_for_function("localStorage.getItem('ut.pid')"); g.wait_for_function("localStorage.getItem('ut.pid')")
    a.fill("#name","Budi"); a.click("#create"); a.wait_for_selector("#room-code:not(:empty)")
    code=a.text_content("#room-code")
    g.fill("#name","Sari"); g.fill("#join-code",code); g.click("#join-form button")
    a.wait_for_function("document.querySelectorAll('#players li:not(.empty)').length==2")
    a.click("#start"); a.wait_for_timeout(1500)
    # both leave back to the lobby by reloading the bare page
    a.goto(B, wait_until="domcontentloaded"); g.goto(B, wait_until="domcontentloaded")
    a.wait_for_selector("#friend-list li", timeout=15000)
    a.wait_for_function("document.querySelector('#friend-list .status-dot.online')", timeout=15000)
    print("A friends:", a.inner_text("#friend-list").replace("\n"," | "))
    a.screenshot(path=f"{S}/24-friends-lobby.png")
    # A makes a new table and invites Sari
    a.fill("#name","Budi"); a.click("#create"); a.wait_for_selector("#room-code:not(:empty)")
    code2=a.text_content("#room-code")
    a.click("#invite-open"); a.wait_for_selector("#invite-list li button:not([disabled])", timeout=10000)
    a.screenshot(path=f"{S}/25-invite-dialog.png")
    a.click("#invite-list li button:not([disabled])")
    a.wait_for_function("document.querySelector('#invite-list button').textContent==='Terkirim'", timeout=5000)
    g.wait_for_selector("#invite-toast:not([hidden])", timeout=10000)
    g.screenshot(path=f"{S}/26-invite-toast-mobile.png")
    print("toast:", g.text_content("#invite-text"))
    g.click("#invite-join")
    a.wait_for_function("[...document.querySelectorAll('#players .who')].some(e=>e.textContent.startsWith('Sari'))", timeout=10000)
    print("Sari joined", code2)
    # transfer Budi's identity to a fresh browser
    a.goto(B, wait_until="domcontentloaded"); a.click("#open-board"); a.click(".transfer summary"); a.click("#make-code")
    a.wait_for_selector("#my-code:not([hidden])"); tcode=a.text_content("#my-code"); print("code", tcode)
    c=page(b,"C"); c.goto(B, wait_until="domcontentloaded"); c.wait_for_function("localStorage.getItem('ut.pid')")
    c.click("#open-board"); c.click(".transfer summary"); c.fill("#claim-code", tcode.lower()); c.click("#claim-form button")
    c.wait_for_function("document.getElementById('transfer-msg').textContent.startsWith('Berhasil')", timeout=5000)
    same = c.evaluate("localStorage.getItem('ut.pid')")==a.evaluate("localStorage.getItem('ut.pid')")
    print("C now has Budi's id:", same)
    c.click("#leaderboard-close"); c.wait_for_selector("#friend-list li", timeout=10000)
    print("C friends:", c.inner_text("#friend-list").replace("\n"," | "))
    c.screenshot(path=f"{S}/27-transfer.png")
    b.close()
print("errors", errs)
