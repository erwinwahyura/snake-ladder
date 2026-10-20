# Stand-in for Google, for scripts/e2e_google_signin.py: approves every
# sign-in at once and returns an ID token for the same test account.
# Run the server with GOOGLE_CLIENT_ID=e2e-client GOOGLE_CLIENT_SECRET=x
# PUBLIC_URL=http://127.0.0.1:8080 GOOGLE_AUTH_URL=http://127.0.0.1:8097/auth
# GOOGLE_TOKEN_URL=http://127.0.0.1:8097/token
import base64, json, time
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlparse, parse_qs, urlencode
class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_GET(self):  # the consent screen: approve at once
        q = parse_qs(urlparse(self.path).query)
        self.send_response(302)
        self.send_header("Location", q["redirect_uri"][0] + "?" + urlencode({"code": "e2e-code", "state": q["state"][0]}))
        self.end_headers()
    def do_POST(self):  # token endpoint
        n = int(self.headers.get("Content-Length", 0)); self.rfile.read(n)
        claims = {"sub": "g-e2e-sari", "given_name": "Sari", "aud": "e2e-client", "iss": "https://accounts.google.com", "exp": int(time.time()) + 3600}
        tok = "e30." + base64.urlsafe_b64encode(json.dumps(claims).encode()).decode().rstrip("=") + ".sig"
        body = json.dumps({"id_token": tok}).encode()
        self.send_response(200); self.send_header("Content-Type", "application/json"); self.end_headers(); self.wfile.write(body)
HTTPServer(("127.0.0.1", 8097), H).serve_forever()
