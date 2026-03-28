#!/usr/bin/env python3
"""HTTP reverse proxy with fault injection for resilience testing.

Control endpoints:
    POST /_fault/on      Enable fault injection (returns 503 for RPC requests)
    POST /_fault/off     Disable fault injection (resume proxying)
    GET  /_fault/status  Return {"enabled": true/false}

All other requests are forwarded to the upstream URL.
"""

import argparse
import json
import threading
import urllib.request
import urllib.error
from http.server import HTTPServer, BaseHTTPRequestHandler


class FaultProxy(BaseHTTPRequestHandler):
    upstream: str = ""
    fault_enabled: bool = False
    lock = threading.Lock()

    def log_message(self, format, *args):
        pass  # suppress request logging

    def do_GET(self):
        if self.path == "/_fault/status":
            self._respond_json({"enabled": self.fault_enabled})
            return
        self._proxy()

    def do_POST(self):
        if self.path == "/_fault/on":
            with self.lock:
                FaultProxy.fault_enabled = True
            self._respond_json({"enabled": True})
            return
        if self.path == "/_fault/off":
            with self.lock:
                FaultProxy.fault_enabled = False
            self._respond_json({"enabled": False})
            return
        self._proxy()

    def _proxy(self):
        if self.fault_enabled:
            self.send_response(503)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"error": "fault injected"}).encode())
            return

        content_length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(content_length) if content_length > 0 else None

        url = self.upstream + self.path
        req = urllib.request.Request(
            url,
            data=body,
            headers={k: v for k, v in self.headers.items() if k.lower() != "host"},
            method=self.command,
        )

        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                resp_body = resp.read()
                self.send_response(resp.status)
                for key, val in resp.getheaders():
                    if key.lower() not in ("transfer-encoding", "connection"):
                        self.send_header(key, val)
                self.end_headers()
                self.wfile.write(resp_body)
        except urllib.error.HTTPError as e:
            self.send_response(e.code)
            self.end_headers()
            self.wfile.write(e.read())
        except Exception as e:
            self.send_response(502)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"error": str(e)}).encode())

    def _respond_json(self, data):
        body = json.dumps(data).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main():
    parser = argparse.ArgumentParser(description="Fault-injecting HTTP reverse proxy")
    parser.add_argument("--upstream", required=True, help="Upstream URL (e.g. http://127.0.0.1:8545)")
    parser.add_argument("--port", type=int, default=9545, help="Port to listen on")
    args = parser.parse_args()

    FaultProxy.upstream = args.upstream.rstrip("/")

    server = HTTPServer(("127.0.0.1", args.port), FaultProxy)
    print(f"Fault proxy listening on :{args.port} -> {args.upstream}")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
