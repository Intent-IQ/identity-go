#!/usr/bin/env python3
"""Local equivalent of the S2S mock used by prebid-go-module."""

import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


RESOLVED_ID = os.environ.get("MOCK_EID", "IIQ-LOCAL-DEV-ID")
BODY = json.dumps({
    "data": {
        "eids": [{
            "source": "intentiq.com",
            "uids": [{"id": RESOLVED_ID, "atype": 1}],
        }],
    },
    "tc": 0,
    "abTestUuid": "local-dev",
    "cttl": 3600,
}).encode()


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(BODY)))
        self.end_headers()
        self.wfile.write(BODY)

    def log_message(self, fmt, *args):
        print("iiq-s2s-api-mock %s" % (fmt % args), flush=True)


if __name__ == "__main__":
    port = int(os.environ.get("PORT", "9098"))
    ThreadingHTTPServer(("0.0.0.0", port), Handler).serve_forever()
