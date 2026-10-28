import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

def start_fake(case: dict) -> tuple[str, dict]:
    bought: dict = {}

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, format: str, *args) -> None:
            pass

        def do_POST(self) -> None:
            size = int(self.headers['content-length'])
            body = json.loads(self.rfile.read(size))
            if self.path == "/shipments":
                response = {
                    "id": "shp_case",
                    "mode": "test",
                    "rates": case["rates"],
                }
                status = 200
            elif self.path.endswith("/buy"):
                bought["rate_id"] = body["rate"]["id"]
                response = {"tracking_code": "EZ4000000004", "mode": "test",
                            "postage_label": {"label_url": "https://example.com/l.png"}
                }
                status = 200
            else:
                response = {"error": "Not found"}
                status = 404
            data = json.dumps(response).encode()
            self.send_response(status)
            self.send_header("Content-type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)


    srv = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    url = f"http://127.0.0.1:{srv.server_port}"
    return url, bought

if __name__ == "__main__":
    url, bought = start_fake({"rates": [{"id": "r1", "carrier": "USPS", "rate": "5.00", "delivery_days": 3}]})
    print(url)
    input("натисни ENter, щоб зупинити")