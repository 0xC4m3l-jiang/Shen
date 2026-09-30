"""假业务站：人工测试的**起点与终点**。

放行的请求原样落到这里（`NI-1`：引擎故障时业务照常），因此它同时是「业务没被影响」的对照。

**必须是多线程的**（`ThreadingHTTPServer`）：本文件用 HTTP/1.1（长连接），而单线程的
`HTTPServer` 一次只服务一个连接 —— 适配器保持长连接后，**其它任何连接都会被挂住**
（实测：改道到本站的幻境后端 10s 无响应、`字节=0`，看起来像「引擎坏了」）。
它是演示物料（不是产品代码），但要让它像真业务站一样能并发，否则测试结论不可信。
"""

import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

BODY = b"<html><body>REAL-BUSINESS</body></html>"


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "text/html")
        self.send_header("Content-Length", str(len(BODY)))
        self.end_headers()
        self.wfile.write(BODY)

    def log_message(self, format, *args):
        """保持安静：访问噪声留给控制台看，不刷终端。"""


def main() -> None:
    if len(sys.argv) < 2 or not sys.argv[1].isdigit():
        raise SystemExit("用法：business.py <port>")
    try:
        server = ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), Handler)
    except OSError as err:  # 端口被占 / 无权限：给出人话，别丢栈
        raise SystemExit(f"business.py: 无法监听端口 {sys.argv[1]}：{err}") from err
    server.serve_forever()


if __name__ == "__main__":
    main()
