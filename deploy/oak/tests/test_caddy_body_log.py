"""使用隔离 Caddy 和回环上游验证正文留存，不访问生产服务或真实供应商。"""
import hashlib
import http.client
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


# 本测试直接启动指定版本，避免系统 PATH 上的旧 Caddy 让升级验证失真。
# CADDY_TEST_BINARY 应为官方发布包校验后的绝对路径。
# 配置通过部署目录相对位置读取，测试与线上候选必须使用同一份文件。
# 所有 HTTP 请求仅发往动态分配的回环端口，不使用真实 API Key。
# 使用标准库 HTTP 客户端，避免外部 SDK 自动压缩或补头影响采集条件。
# 日志文件存在不代表正文已记录，最终断言按响应中的请求 ID 查找正文。
# 日志缺少正文可以是预期策略，但不能改变送往上游的原始内容。
# 大小上限限制的是采集范围；此配置不应该增加业务请求的大小限制。
# 未知长度和带编码请求要保持直通，不能为了记录而强行改变请求格式。
# 内容过滤只针对日志编码，不修改真正发给应用的认证头或查询参数。
# 模型入口以外的 JSON 也要覆盖，防止管理资料被扩大采集。
# 自动 HTTPS、证书续期和 systemd 文件权限由部署后的独立检查负责。
# 日志轮转时间与容量在候选配置校验中确认，不通过修改系统时钟来测试。
# 常规部署测试没有 Caddy 时明确跳过；上线验证必须显式指定候选二进制。
@unittest.skipUnless(os.environ.get("CADDY_TEST_BINARY"), "requires CADDY_TEST_BINARY")
class CaddyBodyLogTest(unittest.TestCase):
    def test_body_recording_contract(self):
        # 握手事件验证因果顺序，不依赖首字节耗时阈值或机器性能。
        # 上游设置有限等待，测试失败也不能永久挂住服务端线程。
        # 流式上游等客户端读到首帧才继续，避免仅验证最终 HTTP 状态。
        release_stream = threading.Event()

        class Upstream(BaseHTTPRequestHandler):
            def do_POST(self):
                # 测试数据不含 chunk 扩展或 trailer，读法只服务此处固定夹具。
                # 这里不是生产 HTTP 解析器，不把简化实现复用到应用中。
                # 覆盖已知长度和 chunked 两类客户端；上游始终验证完整正文摘要。
                body = b""
                if self.headers.get("Transfer-Encoding") == "chunked":
                    while True:
                        size = int(self.rfile.readline().strip(), 16)
                        if size == 0:
                            self.rfile.readline()
                            break
                        body += self.rfile.read(size)
                        self.rfile.read(2)
                else:
                    body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
                # 响应头在转发完成后仍应可供 log_append 读取。
                # 如果错误地提前记录响应头，后续按 ID 查找日志会直接失败。
                # 使用合成关联 ID，测试日志中不涉及生产账号、任务或凭据。
                self.send_response(200)
                self.send_header("X-Oneapi-Request-Id", self.headers["X-Test-Id"])
                if self.path == "/v1/stream":
                    self.send_header("Content-Type", "text/event-stream")
                    self.end_headers()
                    self.wfile.write(b"data: first\n\n")
                    self.wfile.flush()
                    # 若代理缓冲首帧，客户端超时使测试失败，不能假装流式验证通过。
                    release_stream.wait(3)
                    self.wfile.write(b"data: last\n\n")
                else:
                    self.end_headers()
                    self.wfile.write(hashlib.sha256(body).hexdigest().encode())

            def log_message(self, *args):
                # 隔离上游仅提供摘要，不输出正文或测试认证头。
                pass

        # HTTP 服务独占临时端口，异常退出也通过 unittest 清理钩子回收。
        # 不复用生产端口或全局代理，测试可在切换服务之前执行。
        upstream = ThreadingHTTPServer(("127.0.0.1", 0), Upstream)
        thread = threading.Thread(target=upstream.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(upstream.server_close)
        self.addCleanup(upstream.shutdown)
        # 只选择临时回环端口；候选 Caddy 禁用管理 API 和自动 HTTPS。
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        # 日志含合成正文，只存临时目录；成功和失败都会清理此目录。
        # 候选配置不接触运行中 Caddy 的配置、证书目录或管理端口。
        with tempfile.TemporaryDirectory(prefix="oak-caddy-test-") as folder:
            root = Path(folder)
            log = root / "access.jsonl"
            config = (Path(__file__).resolve().parents[1] / "Caddyfile").read_text()
            # 复用待上线配置，只替换站点、上游、输出路径，不另写简化版测试配置。
            config = config.replace("oak.qingshukeji.top", f"http://127.0.0.1:{port}")
            config = config.replace("127.0.0.1:3000", f"127.0.0.1:{upstream.server_port}")
            config = config.replace("/var/log/caddy/oak-access.jsonl", str(log))
            candidate = root / "Caddyfile"
            candidate.write_text("{\n admin off\n auto_https off\n}\n" + config)
            with (root / "runtime.log").open("w+") as runtime:
                process = subprocess.Popen([os.environ["CADDY_TEST_BINARY"], "run", "--config", str(candidate)], stdout=runtime, stderr=runtime)
                try:
                    # 启动就绪后再发行为请求；进程存活本身不作为验收。
                    for _ in range(50):
                        try:
                            with socket.create_connection(("127.0.0.1", port), timeout=0.1):
                                break
                        except OSError:
                            time.sleep(0.05)
                    # 保留本轮排障关心的 input 类型结构，避免只拿纯文本请求冒烟。
                    # 正文比对使用原字符串，能发现重复日志键把正文覆盖为空的问题。
                    payload = b'{"model":"fixture","input":[{"type":"additional_tools","tools":[]}]}'
                    cases = [
                        # 普通正文可按请求 ID 找回，保留 JSON 原始字节。
                        ("recorded", "/v1/responses?key=query-secret&keep=yes", payload, {}, False, True),
                        # 管理及代充值资料不应进入正文日志。
                        ("private", "/api/account-recharge/orders/1/account-info", payload, {}, False, False),
                        # 超限只跳过记录，不拒绝或截断真正发往上游的请求。
                        ("large", "/v1/responses", b'"' + b'x' * 4194304 + b'"', {}, False, False),
                        # 编码正文不在代理层解压记录，原字节仍完整转发。
                        ("encoded", "/v1/responses", payload, {"Content-Encoding": "identity"}, False, False),
                        # 未知长度不做无界缓冲；同样验证上游收到完整请求。
                        ("chunked", "/v1/responses", payload, {}, True, False),
                        # 首帧必须实时到达，日志功能不能缓冲响应。
                        ("stream", "/v1/stream", payload, {}, False, True),
                    ]
                    # 每个用例的 ID 不同，使异步落盘顺序不会影响断言。
                    # 编码测试只检查代理是否原样转发，由上游负责解释编码含义。
                    for ident, path, body, extra, chunked, recorded in cases:
                        headers = {"Content-Type": "application/json", "X-Test-Id": ident,
                                   "Authorization": "Bearer header-secret", "Cookie": "session=cookie-secret",
                                   "X-Api-Key": "api-secret", "X-Goog-Api-Key": "google-secret", **extra}
                        connection = http.client.HTTPConnection("127.0.0.1", port, timeout=2)
                        connection.request("POST", path, iter([body]) if chunked else body, headers, encode_chunked=chunked)
                        response = connection.getresponse()
                        self.assertEqual(response.status, 200)
                        self.assertEqual(response.getheader("X-Oneapi-Request-Id"), ident)
                        if ident == "stream":
                            self.assertEqual(response.readline(), b"data: first\n")
                            release_stream.set()
                            self.assertIn(b"data: last", response.read())
                        else:
                            self.assertEqual(response.read().decode(), hashlib.sha256(body).hexdigest())
                        connection.close()
                    # 等异步访问日志写完再检查；不根据响应成功推断落盘成功。
                    for _ in range(50):
                        rows = [json.loads(line) for line in log.read_text().splitlines()] if log.exists() else []
                        if len(rows) == len(cases):
                            break
                        time.sleep(0.05)
                    # 必须出现所有请求，不能让日志队列遗漏被采集范围判断掩盖。
                    # 日志行之外不导出任何正文到测试输出。
                    by_id = {row["request_id"]: row for row in rows}
                    self.assertEqual(set(by_id), {case[0] for case in cases})
                    for ident, _, body, _, _, recorded in cases:
                        # 同时覆盖采集和排除路径，缺字段代表按策略跳过正文。
                        self.assertEqual("request_body" in by_id[ident], recorded)
                        if recorded:
                            self.assertEqual(by_id[ident]["request_body"], body.decode())
                    # 默认脱敏之外再验证常见 API Key 头及 URL 查询凭据确实移除。
                    # 凭据标记只用于验证剔除规则，不是可用账号或密钥。
                    # 同时保留无敏感查询参数，避免简单删除整个 URI 假装脱敏成功。
                    stored = log.read_text()
                    for secret in ["header-secret", "cookie-secret", "api-secret", "google-secret", "query-secret"]:
                        self.assertNotIn(secret, stored)
                    self.assertIn("keep=yes", by_id["recorded"]["request"]["uri"])
                    self.assertEqual(log.stat().st_mode & 0o777, 0o600)
                finally:
                    # 只清理本次隔离进程，失败时输出 Caddy 自身诊断而非请求正文。
                    release_stream.set()
                    process.terminate()
                    process.wait(timeout=5)
                    if process.returncode not in (0, -15):
                        runtime.seek(0)
                        print(runtime.read())


if __name__ == "__main__":
    unittest.main()
