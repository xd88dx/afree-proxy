#!/usr/bin/env python3
"""平台可用性自动检测（随镜像分发，启动时播种到数据目录 scripts/）。

用法：python platform_audit.py -P <平台前缀>  如 -P "crow:" / -P "tacn:"；
结果表格落脚本同目录 audit/<前缀>-audit.txt（固定名覆盖，只留最新）。

流程：自动发现清单 → 逐模型 plain + agent(10工具+stream) 两维度实测
→ 冷却连锁/限流/空输出自动定性复测 → 结果表格落 audit/<前缀>-audit.txt。
纯只读：不发任何配置修改请求。
"""
import argparse
import json
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime
from pathlib import Path


def agent_tools():
    def tool(n, d, p, req=None):
        return {"type": "function", "function": {"name": n, "description": d,
                "parameters": {"type": "object", "properties": p, "required": req or list(p)}}}
    return [
        tool("write_file", "Write text content to a file.", {"path": {"type": "string"}, "content": {"type": "string"}}),
        tool("read_file", "Read a file from the filesystem.", {"path": {"type": "string"}}),
        tool("bash", "Run a shell command.", {"command": {"type": "string"}, "timeout": {"type": "integer"}}, ["command"]),
        tool("grep", "Search file contents with regex.", {"pattern": {"type": "string"}, "path": {"type": "string"}}, ["pattern"]),
        tool("glob", "Find files matching a glob pattern.", {"pattern": {"type": "string"}}),
        {"type": "function", "function": {"name": "edit_file", "description": "Edit a file with either simple replacement or structured edits.",
         "parameters": {"type": "object", "properties": {"edit": {"oneOf": [
             {"type": "object", "properties": {"old": {"type": "string"}, "new": {"type": "string"}}, "required": ["old", "new"]},
             {"type": "object", "properties": {"hunks": {"type": "array", "items": {"type": "object",
              "properties": {"start": {"type": "integer"}, "text": {"type": "string"}}, "required": ["start", "text"]}}}, "required": ["hunks"]}]},
          "path": {"type": "string"}}, "required": ["path", "edit"]}}},
        tool("webfetch", "Fetch a URL and return its content.", {"url": {"type": "string"}}, ["url"]),
        tool("todo_write", "Write the todo list.", {"items": {"type": "array", "items": {"type": "object",
         "properties": {"id": {"type": "string"}, "status": {"type": "string", "enum": ["pending", "in_progress", "done"]}},
         "required": ["id", "status"]}}}),
        tool("task", "Launch a subagent task.", {"description": {"type": "string"}, "prompt": {"type": "string"}}, ["prompt", "description"]),
        tool("list", "List directory contents.", {"path": {"type": "string"}}),
    ]


def http_post(url, key, body, timeout):
    req = urllib.request.Request(url + "/v1/chat/completions", data=json.dumps(body).encode(), method="POST",
                                 headers={"Authorization": "Bearer " + key, "Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            raw = r.read().decode(errors="replace")
            if "text/event-stream" in (r.headers.get("Content-Type") or ""):
                return r.status, parse_sse(raw)
            return r.status, json.loads(raw)
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode(errors="replace")[:300]
    except Exception as e:
        return None, str(e)[:200]


def parse_sse(raw):
    content, reasoning, err = [], [], None
    for line in raw.splitlines():
        if not line.startswith("data: "):
            continue
        payload = line[6:]
        if payload == "[DONE]":
            break
        try:
            d = json.loads(payload)
        except ValueError:
            continue
        if d.get("error"):
            err = d["error"]
            break
        for ch in d.get("choices") or []:
            delta = ch.get("delta") or {}
            if delta.get("content"):
                content.append(delta["content"])
            if delta.get("reasoning_content"):
                reasoning.append(delta["reasoning_content"])
    return {"content": "".join(content), "reasoning": "".join(reasoning), "error": err}


def is_no_key(status, data):
    return status == 502 and isinstance(data, str) and "no " in data and "key routable" in data


def probe_once(gw, key, model, mode, timeout):
    body = {"model": model, "messages": [{"role": "user", "content": "Reply with exactly: OK"}], "max_tokens": 256}
    if mode == "agent":
        body.update({"tools": agent_tools(), "stream": True, "max_tokens": 64})
    status, data = http_post(gw, key, body, timeout)
    if status != 200:
        return status, data
    if mode == "agent" and data.get("error"):
        return -1, json.dumps(data["error"], ensure_ascii=False)[:300]
    return 200, data


def deep_probe(gw, key, model, timeout):
    """200 但 content/reasoning 全空时用大预算复测定性（推理模型 vs 上游空输出）。"""
    body = {"model": model, "messages": [{"role": "user", "content": "Reply with exactly: OK"}], "max_tokens": 2048}
    status, data = http_post(gw, key, body, timeout)
    if status == 200 and isinstance(data, dict):
        m = (data.get("choices") or [{}])[0].get("message", {})
        if (m.get("content") or "").strip() or (m.get("reasoning_content") or "").strip():
            return True, "大预算(2048)复测有内容：推理模型正常输出"
    return False, "大预算(2048)复测仍全空 → 上游空输出异常"


def classify(status, body):
    b = body or ""
    if status == 404:
        return "not_served", "目录无此形态（映射/勾选问题）"
    if "524" in b:
        return "cf_524", "Cloudflare 524 源站超时（复测稳定）"
    if "429" in b or "Rate limit" in b:
        return "rate_limited", "上游限流（复测仍撞）"
    if status is None:
        return "network", "连接层失败(EOF/超时): " + b[:80]
    return "upstream_5xx", f"HTTP {status}: {b[:110]}"


def audit_model(gw, key, model, timeout, interval, log):
    verdict = {}
    for mode in ("plain", "agent"):
        attempts, tag, note = 0, None, None
        while True:
            attempts += 1
            status, data = probe_once(gw, key, model, mode, timeout)
            if is_no_key(status, data) and attempts <= 3:
                log(f"    [{mode}] key 池全冷却，等 75s 重试（{attempts}/3）")
                time.sleep(75)
                continue
            if status == 200:
                if mode == "plain":
                    c = data.get("content", "")
                    rsn = data.get("reasoning_content") or data.get("reasoning") or ""
                    if c.strip() or rsn.strip():
                        tag, note = "ok", f"正常输出（{len(c)}字）"
                    else:
                        ok, note = deep_probe(gw, key, model, timeout)
                        tag = "ok" if ok else "upstream_empty_output"
                        if ok:
                            note = "ok：" + note
                    break
                tag, note = "ok", "agent 负载(10工具+stream)被接受，流正常"
                break
            if attempts <= 3:
                wait = 20 * attempts
                log(f"    [{mode}] {status}，{wait}s 后复测（{attempts}/3）")
                time.sleep(wait)
                continue
            body = data if isinstance(data, str) else json.dumps(data, ensure_ascii=False)[:300]
            tag, note = classify(status, body)
            break
        verdict[mode] = {"tag": tag, "note": note, "attempts": attempts}
        log(f"    [{mode}] -> {tag} ({note[:80]})")
        time.sleep(interval)
    return verdict


def write_report(outdir, prefix, gw, results, models):
    outdir.mkdir(parents=True, exist_ok=True)
    clean = prefix.strip().strip(":/").replace("/", "-") or "audit"
    path = outdir / f"{clean}-audit.txt"
    lines = []
    w_model = max([len(m) for m in models] + [len("模型")]) + 2
    lines.append(f"{clean} 平台可用性检测  {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    lines.append(f"网关: {gw}   模型数: {len(models)}")
    lines.append("=" * (w_model + 34))
    lines.append(f"{'模型':<{w_model}}{'plain':<24}{'agent':<24}结论")
    lines.append("-" * (w_model + 34))
    for m in models:
        v = results[m]
        mark = "✅" if all(x["tag"] == "ok" for x in v.values()) else "❌"
        lines.append(f"{m:<{w_model}}{v['plain']['tag']:<24}{v['agent']['tag']:<24}{mark}")
    lines.append("=" * (w_model + 34))
    notes = [(m, mode, x) for m in models for mode, x in results[m].items() if x["tag"] != "ok"]
    if notes:
        lines.append("说明:")
        for m, mode, x in notes:
            lines.append(f"- {m} [{mode}] {x['note']}（复测 {x['attempts']} 次）")
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    return path


def main():
    ap = argparse.ArgumentParser(description="平台可用性自动检测")
    ap.add_argument("-P", dest="prefix", help="平台前缀，例如 crow: / tacn: / zen:")
    ap.add_argument("--gateway", default="http://127.0.0.1:3457")
    ap.add_argument("--api-key", default="8008208820")
    ap.add_argument("--timeout", type=int, default=150)
    ap.add_argument("--interval", type=float, default=10)
    ap.add_argument("--single", default="", help="只测指定模型（调试用）")
    args = ap.parse_args()
    if not args.prefix or not args.prefix.strip():
        print("请 -P 指定平台前缀")
        sys.exit(1)

    def log(msg):
        print(msg, flush=True)

    req = urllib.request.Request(args.gateway + "/v1/models", headers={"Authorization": "Bearer " + args.api_key})
    with urllib.request.urlopen(req, timeout=15) as r:
        ids = [m["id"] for m in json.loads(r.read().decode())["data"]]
    models = [args.single] if args.single else sorted(i for i in ids if i.startswith(args.prefix))
    if not models:
        log(f"清单为空：网关 {args.gateway} 下没有 {args.prefix} 前缀的模型")
        sys.exit(2)
    log(f"清单：{len(models)} 个模型（prefix={args.prefix}）")

    results = {}
    for m in models:
        log(f"  {m}")
        results[m] = audit_model(args.gateway, args.api_key, m, args.timeout, args.interval, log)

    outdir = Path(__file__).resolve().parent / "audit"
    path = write_report(outdir, args.prefix, args.gateway, results, models)

    log("\n===== 结果矩阵 =====")
    for m, v in results.items():
        states = " / ".join(f"{mode}:{x['tag']}" for mode, x in v.items())
        mark = "✅" if all(x["tag"] == "ok" for x in v.values()) else "❌"
        log(f"{mark} {m}  [{states}]")
    log(f"存档：{path}")


if __name__ == "__main__":
    main()
