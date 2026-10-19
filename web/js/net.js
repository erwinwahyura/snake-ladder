// WebSocket client that reconnects with the seat token after a drop
// (phones sleeping, switching apps, flaky mobile data).
export class Connection {
  constructor({ onMessage, onStatus }) {
    this.onMessage = onMessage;
    this.onStatus = onStatus;
    this.params = null;
    this.ws = null;
    this.retry = 0;
    this.closedByUs = false;
    this.fatal = false;
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible' && this.params && !this.isOpen() && !this.fatal) this.open();
    });
  }

  // params is a function so each reconnect picks up the latest token.
  connect(params) {
    this.params = params;
    this.closedByUs = false;
    this.fatal = false;
    this.open();
  }

  isOpen() {
    return this.ws && this.ws.readyState === WebSocket.OPEN;
  }

  open() {
    clearTimeout(this.timer);
    if (this.ws) {
      this.ws.onclose = null;
      this.ws.close();
    }
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    const qs = new URLSearchParams(Object.entries(this.params()).filter(([, v]) => v));
    const ws = new WebSocket(`${proto}://${location.host}/ws?${qs}`);
    this.ws = ws;
    this.onStatus(this.retry ? 'reconnecting' : 'connecting');
    ws.onopen = () => {
      this.retry = 0;
      this.onStatus('open');
    };
    ws.onmessage = (e) => {
      let msg;
      try {
        msg = JSON.parse(e.data);
      } catch {
        return;
      }
      if (msg.t === 'error' && (msg.code === 'room_not_found' || msg.code === 'server_full')) {
        this.fatal = true;
      }
      this.onMessage(msg);
    };
    ws.onclose = () => {
      if (this.closedByUs || this.fatal) {
        this.onStatus('closed');
        return;
      }
      this.retry++;
      const delay = Math.min(8000, 400 * 2 ** this.retry);
      this.onStatus('reconnecting');
      this.timer = setTimeout(() => this.open(), delay);
    };
  }

  send(msg) {
    if (this.isOpen()) this.ws.send(JSON.stringify(msg));
  }

  close() {
    this.closedByUs = true;
    clearTimeout(this.timer);
    if (this.ws) this.ws.close();
  }
}
