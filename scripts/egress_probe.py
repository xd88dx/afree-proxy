#!/usr/bin/env python3
"""出口 × 平台端点 分层连通性探测（随镜像分发，启动时播种到数据目录 scripts/）。

用法：
  python egress_probe.py --exit "DDPHX=socks5://10.10.10.5:10106" --exit "Ulzix=socks5://10.10.10.5:10110"
  python egress_probe.py --proxy-config <proxy-config.json>   # 池内全部出口（解析 #alias 与 user:pass）
  可选: --targets cb-page,cb-api 只测部分目标  --timeout 15  --interval 0.5  --insecure

结果落脚本同目录 probe/egress-probe.txt（矩阵）与 probe/egress-probe.tsv（原始行），固定名覆盖。

分层归因（每个 出口×目标）：
  1. 出口画像: 经出口查 IP/国家 + 控制组 gstatic/github，两者皆败判 DEAD 跳过该出口
  2. L1: SOCKS5 CONNECT 发域名（出口侧解析）→ TLS → HTTP GET
  3. L1 传输层失败 → L2: 经出口打 1.1.1.1 DoH（IP 字面量，自身免 DNS）取 A 记录
     → L3: CONNECT 发 IP（绕开出口 DNS）重测
  4. 归因: L3 通 L1 不通=DNS_BLK(节点DNS黑洞)；L1/L3 皆败=TCP_BLK(平台封或跨境阻断)；
     403 带 WAF/CF 签名=WAF_403(HTTP层封出口IP)；TLS 失败=TLS_ERR；SOCKS 回复码=SOCKS_<码>
纯只读探测：不带任何凭据、不发真实聊天请求。
"""
import argparse
import json
import socket
import ssl
import struct
import sys
import time
from datetime import datetime
from pathlib import Path

# 平台探测目标：(标签, 主机, 路径)。控制组（前两项）打头，用于判定出口本身死活。
TARGETS = [
    ("control-gstatic", "www.gstatic.com", "/generate_204"),
    ("control-github", "api.github.com", "/zen"),
    ("wb-cn-page", "www.workbuddy.cn", "/"),
    ("wb-cn-api", "www.workbuddy.cn", "/v2/chat/completions"),
    ("cb-page", "www.codebuddy.ai", "/"),
    ("cb-api", "www.codebuddy.ai", "/console/chat/completions"),
    ("trae-cn-api", "trae-api-cn.mchost.guru", "/api/ide/v1/chat"),
    ("trae-cn-oauth", "api.trae.cn", "/"),
    ("trae-cn-login", "www.trae.cn", "/"),
    ("trae-intl-api", "core-normal.trae.ai", "/"),
    ("trae-intl-oauth", "ug-normal.trae.ai", "/"),
    ("trae-intl-login", "www.trae.ai", "/"),
    ("cline", "api.cline.bot", "/api/v1/ai/cline/recommended-models"),
    ("zen-api", "opencode.ai", "/inference/openai/v1/models"),
    ("zen-catalog", "models.opencode.ai", "/api.json"),
    ("oprt", "openrouter.ai", "/api/v1/models"),
    ("tkhb", "tokenharbor.ai", "/v1/models"),
    ("amd", "developer.amd.com.cn", "/radeon/api/v1/models"),
]

SOCKS_ERR = {0: "succeeded", 1: "general_failure", 2: "not_allowed",
             3: "network_unreachable", 4: "host_unreachable", 5: "connection_refused",
             6: "ttl_expired", 7: "command_not_supported", 8: "address_type_not_supported"}


class SocksError(Exception):
    def __init__(self, code):
        self.code = code
        super().__init__(SOCKS_ERR.get(code, f"reply_{code}"))


class TransportError(Exception):
    """传输层失败（握手/认证/连接），消息短串直接进矩阵 note。"""


def parse_proxy(url):
    """socks5://user:pass@host:port#alias -> dict；无 scheme 时按 host:port 容错。"""
    alias = ""
    if "#" in url:
        url, alias = url.split("#", 1)
        alias = alias.strip()
    if "://" in url:
        scheme, rest = url.split("://", 1)
        if scheme not in ("socks5", "socks5h"):
            raise ValueError(f"仅支持 socks5 出口，收到 {scheme}://")
    else:
        rest = url
    auth = None
    if "@" in rest:
        cred, rest = rest.rsplit("@", 1)
        user, _, pwd = cred.partition(":")
        auth = (user, pwd)
    host, _, port = rest.partition(":")
    if not host or not port:
        raise ValueError(f"出口地址不完整: {url}")
    return {"alias": alias or f"{host}:{port}", "host": host,
            "port": int(port), "auth": auth}


def _recv_exact(sock, n):
    buf = b""
    while len(buf) < n:
        c = sock.recv(n - len(buf))
        if not c:
            raise TransportError("socks_eof")
        buf += c
    return buf


def socks5_connect(px, dst, port, timeout, send_domain):
    """建 SOCKS5 隧道。send_domain=True 把域名交给出口解析（socks5h 语义）；
    False 时 dst 必须是 IP 字面量（绕开出口 DNS）。返回已握手 socket。"""
    sock = socket.create_connection((px["host"], px["port"]), timeout=timeout)
    sock.settimeout(timeout)
    try:
        auth = px["auth"]
        sock.sendall(b"\x05\x02\x00\x02" if auth else b"\x05\x01\x00")
        r = _recv_exact(sock, 2)
        if r[0] != 5:
            raise TransportError("socks_greeting_bad")
        if r[1] == 2 and auth:
            u, p = auth[0].encode(), auth[1].encode()
            sock.sendall(b"\x01" + bytes([len(u)]) + u + bytes([len(p)]) + p)
            if _recv_exact(sock, 2)[1] != 0:
                raise TransportError("socks_auth_failed")
        elif r[1] != 0:
            raise TransportError("socks_no_acceptable_auth")
        if send_domain:
            d = dst.encode()
            req = b"\x05\x01\x00\x03" + bytes([len(d)]) + d + struct.pack(">H", port)
        else:
            req = b"\x05\x01\x00\x01" + socket.inet_aton(dst) + struct.pack(">H", port)
        sock.sendall(req)
        rep = _recv_exact(sock, 4)
        if rep[1] != 0:
            raise SocksError(rep[1])
        atyp = rep[3]
        if atyp == 1:
            _recv_exact(sock, 6)
        elif atyp == 3:
            n = _recv_exact(sock, 1)[0]
            _recv_exact(sock, n + 2)
        elif atyp == 4:
            _recv_exact(sock, 18)
        return sock
    except socket.timeout:
        try:
            sock.close()
        except OSError:
            pass
        raise TransportError("socks_timeout") from None
    except Exception:
        try:
            sock.close()
        except OSError:
            pass
        raise


def http_over(sock, host, port, path, timeout, insecure, extra=None):
    """隧道上 HTTP(S) GET。返回 (status, headers, body, err)；err 非 None 即传输层失败。"""
    try:
        if port == 443:
            ctx = ssl.create_default_context()
            if insecure:
                ctx.check_hostname = False
                ctx.verify_mode = ssl.CERT_NONE
            sock = ctx.wrap_socket(sock, server_hostname=host)
        lines = [f"GET {path} HTTP/1.1", f"Host: {host}",
                 "User-Agent: afree-egress-probe/1.0", "Accept: */*", "Connection: close"]
        for k, v in (extra or {}).items():
            lines.append(f"{k}: {v}")
        sock.sendall(("\r\n".join(lines) + "\r\n\r\n").encode())
        buf = b""
        while b"\r\n\r\n" not in buf and len(buf) < 32768:
            c = sock.recv(4096)
            if not c:
                break
            buf += c
        if b"\r\n\r\n" not in buf:
            return None, {}, b"", "no_http_response"
        head, _, body = buf.partition(b"\r\n\r\n")
        hl = head.decode(errors="replace").split("\r\n")
        parts = hl[0].split()
        status = int(parts[1]) if len(parts) >= 2 and parts[1].isdigit() else None
        headers = {}
        for ln in hl[1:]:
            k, _, v = ln.partition(":")
            headers[k.strip().lower()] = v.strip()
        try:
            while len(body) < 4096:
                c = sock.recv(4096)
                if not c:
                    break
                body += c
        except OSError:
            pass
        return status, headers, body[:4096], None
    except ssl.SSLError as e:
        return None, {}, b"", "tls_" + str(getattr(e, "reason", "") or e)[:40].replace(" ", "_")
    except (socket.timeout, TimeoutError):
        return None, {}, b"", "timeout"
    except OSError as e:
        return None, {}, b"", "os_" + str(e)[:50].replace(" ", "_")
    finally:
        try:
            sock.close()
        except OSError:
            pass


def classify_http(status, headers, body):
    """HTTP 层定性：403 且带 WAF/CF 签名 = WAF_403（HTTP 层封出口 IP）；
    其余状态码都算链路可达（401/404/405 是无凭据 GET 的正常业务响应）。"""
    if status is None:
        return "NO_HTTP"
    if status == 403:
        h = " ".join(f"{k}={v}" for k, v in headers.items()).lower()
        b = body.decode(errors="replace").lower()
        if ("cloudflare" in h or "cf-mitigated" in h or "apisix" in b
                or "just a moment" in b or "attention required" in b or "waf" in b):
            return "WAF_403"
    return f"HTTP_{status}"


def doh_resolve(px, host, timeout, insecure):
    """经出口打 IP 字面量 DoH（自身免 DNS，黑洞免疫），返回该域名 A 记录列表或 []。"""
    for (ip, port), sni, qpath in (
        (("1.1.1.1", 443), "cloudflare-dns.com", "/dns-query?name=%s&type=A"),
        (("8.8.8.8", 443), "dns.google", "/resolve?name=%s&type=A"),
    ):
        try:
            sk = socks5_connect(px, ip, port, timeout, send_domain=False)
            st, hd, body, err = http_over(sk, sni, port, qpath % host, timeout, insecure,
                                          extra={"accept": "application/dns-json"})
            if err or st != 200:
                continue
            ans = json.loads(body.decode(errors="replace")).get("Answer") or []
            ips = [a["data"] for a in ans if a.get("type") == 1]
            if ips:
                return ips
        except (SocksError, TransportError, OSError, ValueError):
            continue
    return []


def probe_target(px, host, path, timeout, insecure, doh_cache):
    """单出口×单目标探测。返回 (tag, status, latency, note)。
    L1=CONNECT 发域名（出口侧解析）；L1 任何传输层失败（含 CONNECT 假成功后
    数据黑洞）都走 DoH 问路 + L3 发 IP，区分「节点 DNS 黑洞」与「TCP 层封」。"""
    t0 = time.monotonic()
    err1 = None
    try:
        sk = socks5_connect(px, host, 443, timeout, send_domain=True)
    except SocksError as e:
        err1 = f"SOCKS_{e.code}"
    except TransportError as e:
        err1 = str(e)
    except OSError as e:
        err1 = "os_" + str(e)[:40].replace(" ", "_")
    else:
        st, hd, body, herr = http_over(sk, host, 443, path, timeout, insecure)
        dt = round(time.monotonic() - t0, 1)
        if herr is None:
            return classify_http(st, hd, body), st, dt, ""
        if herr.startswith("tls"):
            return "TLS_ERR", None, dt, herr
        err1 = herr  # CONNECT 通但无 HTTP 响应：数据黑洞，继续分层判别
    # 分层：DoH 定位真实 IP 后发 IP 重测
    key = (px["alias"], host)
    ips = doh_cache.get(key)
    if ips is None:
        ips = doh_resolve(px, host, timeout, insecure)
        doh_cache[key] = ips
    if not ips:
        if err1.startswith(("SOCKS", "os_", "socks")):
            tag = err1
        else:
            tag = "HTTP_TIMEOUT" if err1 == "timeout" else "HTTP_EOF"
        return tag, None, None, "DoH亦不通(出口网络严重异常)"
    note = f"L1:{err1}"
    try:
        sk = socks5_connect(px, ips[0], 443, timeout, send_domain=False)
    except SocksError as e:
        return "TCP_BLK", None, None, f"{note};L3:{e}"
    except (TransportError, OSError) as e:
        return "TCP_BLK", None, None, f"{note};L3:{str(e)[:50]}"
    st, hd, body, herr = http_over(sk, host, 443, path, timeout, insecure)
    dt = round(time.monotonic() - t0, 1)
    if herr is None:
        return "DNS_BLK", st, dt, f"{note};L3:{st}(经IP数据通,出口对该域名解析坏)"
    if herr.startswith("tls"):
        return "TLS_ERR", None, dt, f"{note};L3:{herr}"
    return "TCP_BLK", None, dt, f"{note};L3:{herr}"


def probe_exit_profile(px, timeout, insecure):
    """经出口查出口 IP/国家/ISP（ip-api 明文 80，失败回落 api.ip.sb）。返回短串。"""
    try:
        sk = socks5_connect(px, "ip-api.com", 80, timeout, send_domain=True)
        st, hd, body, err = http_over(
            sk, "ip-api.com", 80, "/json?fields=query,country,isp", timeout, insecure)
        if st == 200:
            d = json.loads(body.decode(errors="replace"))
            return f"{d.get('query','')} {d.get('country','')} {d.get('isp','')}".strip()
    except (SocksError, TransportError, OSError, ValueError):
        pass
    try:
        sk = socks5_connect(px, "api.ip.sb", 443, timeout, send_domain=True)
        st, hd, body, err = http_over(sk, "api.ip.sb", 443, "/geoip", timeout, insecure)
        if st == 200:
            d = json.loads(body.decode(errors="replace"))
            return f"{d.get('ip','')} {d.get('country','')} {d.get('isp', d.get('organization',''))}".strip()
    except (SocksError, TransportError, OSError, ValueError):
        pass
    return ""


def write_report(outdir, targets, proxies, profiles, results, dead):
    """矩阵（行=目标、列=出口）+ 失败明细落 egress-probe.txt；原始行落 egress-probe.tsv。"""
    outdir.mkdir(parents=True, exist_ok=True)
    names = [p["alias"] for p in proxies]
    w_label = max(len(t[0]) for t in targets) + 2
    cols = [max(len(n), 12) + 2 for n in names]
    lines = [f"出口 × 平台端点 分层探测  {datetime.now():%Y-%m-%d %H:%M:%S}",
             "失败类别: DEAD=出口死  DNS_BLK=节点DNS黑洞  TCP_BLK=TCP层封  TLS_ERR=TLS失败",
             "          WAF_403=HTTP层封出口IP  SOCKS_n=SOCKS回复码  HTTP_EOF/TIMEOUT=CONNECT通但无HTTP响应",
             "          HTTP_x/200/204=链路可达（401/404/405 是无凭据 GET 的正常业务响应）", ""]
    for px in proxies:
        lines.append(f"[{px['alias']}] 出口IP: {profiles.get(px['alias']) or '未知'}"
                     + ("  [DEAD]" if px["alias"] in dead else ""))
    lines.append("")
    lines.append("目标".ljust(w_label) + "".join(n.ljust(c) for n, c in zip(names, cols)))
    lines.append("-" * (w_label + sum(cols)))
    for label, _host, _path in targets:
        row = label.ljust(w_label)
        for px, c in zip(proxies, cols):
            r = results.get((px["alias"], label))
            cell = "-" if r is None else (r["tag"] + (f"/{r['latency']}s" if r["latency"] else ""))
            row += cell[:c - 1].ljust(c)
        lines.append(row)
    notes = [(a, l, r["note"]) for (a, l), r in sorted(results.items()) if r["note"]]
    if notes:
        lines.append("")
        lines.append("失败明细:")
        for a, l, note in notes:
            lines.append(f"- {a} × {l}: {note}")
    p = outdir / "egress-probe.txt"
    p.write_text("\n".join(lines) + "\n", encoding="utf-8")
    lookup = {t[0]: t for t in targets}
    tsv = outdir / "egress-probe.tsv"
    with tsv.open("w", encoding="utf-8", newline="") as f:
        f.write("exit\ttarget\thost\tpath\ttag\tstatus\tlatency\tnote\n")
        for (a, l), r in sorted(results.items()):
            t = lookup[l]
            f.write(f"{a}\t{l}\t{t[1]}\t{t[2]}\t{r['tag']}\t{r['status'] or ''}"
                    f"\t{r['latency'] or ''}\t{r['note'].replace(chr(9), ' ').replace(chr(10), ' ')}\n")
    return p


def main():
    ap = argparse.ArgumentParser(description="出口 × 平台端点 分层连通性探测")
    ap.add_argument("--exit", dest="exits", action="append", default=[],
                    help='NAME=socks5://host:port（可重复；省略 NAME= 时用 host:port 当名）')
    ap.add_argument("--proxy-config", default="",
                    help="proxy-config.json 路径：读 proxies 数组全池探测（解析 #alias 与 user:pass）")
    ap.add_argument("--targets", default="", help="逗号分隔目标标签过滤，缺省全部")
    ap.add_argument("--timeout", type=int, default=15)
    ap.add_argument("--interval", type=float, default=0.5)
    ap.add_argument("--insecure", action="store_true", help="跳过 TLS 证书校验")
    args = ap.parse_args()

    def log(msg):
        print(msg, flush=True)

    proxies = []
    for item in args.exits:
        name, _, url = item.partition("=")
        if not url:
            url, name = name, ""
        p = parse_proxy(url.strip())
        if name.strip():
            p["alias"] = name.strip()
        proxies.append(p)
    if args.proxy_config:
        cfg = json.loads(Path(args.proxy_config).read_text(encoding="utf-8"))
        for line in cfg.get("proxies") or []:
            try:
                proxies.append(parse_proxy(str(line).strip()))
            except ValueError as e:
                log(f"跳过无法解析的出口行: {e}")
    if not proxies:
        print("请用 --exit 或 --proxy-config 指定出口")
        sys.exit(1)
    targets = TARGETS
    if args.targets:
        want = {t.strip() for t in args.targets.split(",") if t.strip()}
        targets = [t for t in TARGETS if t[0] in want]
        if not targets:
            print(f"--targets 没有匹配任何内置目标，可选: {','.join(t[0] for t in TARGETS)}")
            sys.exit(1)
    ctrl = [t[0] for t in TARGETS[:2]]

    log(f"出口 {len(proxies)} 个 × 目标 {len(targets)} 个，串行探测（timeout={args.timeout}s）")
    profiles, results, dead, doh_cache = {}, {}, set(), {}
    for px in proxies:
        a = px["alias"]
        log(f"\n== 出口 {a} ({px['host']}:{px['port']})")
        profiles[a] = probe_exit_profile(px, args.timeout, args.insecure)
        log(f"   出口IP: {profiles[a] or '未知'}")
        # 先测控制组判定出口死活，DEAD 出口跳过其余目标（省超时等待）
        is_dead = False
        for label, host, path in targets:
            if label not in ctrl and is_dead:
                continue
            tag, status, lat, note = probe_target(
                px, host, path, args.timeout, args.insecure, doh_cache)
            results[(a, label)] = {"tag": tag, "status": status,
                                   "latency": lat, "note": note}
            extra = f"  ({note})" if note else ""
            log(f"   {label:<16} {tag}" + (f" HTTP {status}" if status else "")
                + (f"  {lat}s" if lat else "") + extra)
            if label == ctrl[1] and all(
                    results[(a, c)]["tag"].startswith(("SOCKS", "os_", "socks", "TCP"))
                    for c in ctrl):
                dead.add(a)
                is_dead = True
                log("   !! 控制组全败，出口判 DEAD，跳过其余目标")
            time.sleep(args.interval)
    # DEAD 出口未测的目标格补记
    for px in proxies:
        if px["alias"] not in dead:
            continue
        for label, _h, _p in targets:
            if (px["alias"], label) not in results:
                results[(px["alias"], label)] = {
                    "tag": "DEAD", "status": None, "latency": None, "note": "出口控制组全败"}
    path = write_report(Path(__file__).resolve().parent / "probe",
                        targets, proxies, profiles, results, dead)
    log("\n===== 结果矩阵 =====")
    log(path.read_text(encoding="utf-8"))
    log(f"存档: {path}")


if __name__ == "__main__":
    main()










