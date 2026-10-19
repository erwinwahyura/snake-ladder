// Presence socket: while it is open you show as online to your Teman main
// and can receive invites. It only listens; it reconnects on its own.
export class Presence {
  constructor({ getPid, onInvite }) {
    this.getPid = getPid;
    this.onInvite = onInvite;
    this.retry = 0;
    this.ws = null;
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible' && !this.isOpen()) this.open();
    });
  }

  isOpen() {
    return this.ws && this.ws.readyState <= WebSocket.OPEN;
  }

  // Call again after the player ID changes (new device, new key).
  restart() {
    if (this.ws) {
      this.ws.onclose = null;
      this.ws.close();
    }
    this.retry = 0;
    this.open();
  }

  open() {
    clearTimeout(this.timer);
    const pid = this.getPid();
    if (!pid) return;
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    const ws = new WebSocket(`${proto}://${location.host}/ws/presence?pid=${encodeURIComponent(pid)}`);
    this.ws = ws;
    ws.onopen = () => (this.retry = 0);
    ws.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data);
        if (msg.t === 'invite') this.onInvite(msg);
      } catch {
        /* ignore */
      }
    };
    ws.onclose = () => {
      this.retry++;
      this.timer = setTimeout(() => this.open(), Math.min(30000, 1000 * 2 ** this.retry));
    };
  }
}
