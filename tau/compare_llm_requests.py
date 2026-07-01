#!/usr/bin/env python3
from __future__ import annotations

import argparse
import difflib
import json
import sys
from pathlib import Path
from typing import Any


def main() -> None:
    args = parse_args()
    official = load_official_requests(args.official_run_dir, args.task_id)
    trpc = load_trpc_requests(args.trpc_run_dir)
    if not official:
        raise SystemExit(f"No official agent_response logs found under {args.official_run_dir}")
    if not trpc:
        raise SystemExit(f"No trpc request logs found under {args.trpc_run_dir}")
    failures = 0
    if args.all and len(official) != len(trpc):
        print(f"request_count=mismatch official={len(official)} trpc={len(trpc)}")
        failures += 1
    pairs = min(len(official), len(trpc)) if args.all else 1
    start = args.request_index
    for offset in range(pairs):
        index = start + offset
        if index >= len(official) or index >= len(trpc):
            raise SystemExit(f"Request index {index} is out of range: official={len(official)} trpc={len(trpc)}")
        official_path, official_request = official[index]
        trpc_path, trpc_request = trpc[index]
        left = normalize_request(
            official_request,
            ignore_user_content=args.ignore_user_content,
            ignore_schema_title=args.ignore_schema_title,
        )
        right = normalize_request(
            trpc_request,
            ignore_user_content=args.ignore_user_content,
            ignore_schema_title=args.ignore_schema_title,
        )
        print(f"request_index={index}")
        print(f"official={official_path}")
        print(f"trpc={trpc_path}")
        if left == right:
            print("status=match")
            continue
        failures += 1
        print("status=mismatch")
        print_diff(left, right)
    if failures:
        raise SystemExit(1)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Compare official tau2 and trpc-agent-go LLM request contexts.")
    parser.add_argument("official_run_dir", type=Path)
    parser.add_argument("trpc_run_dir", type=Path)
    parser.add_argument("--task-id", default="")
    parser.add_argument("--request-index", type=int, default=0)
    parser.add_argument("--all", action="store_true")
    parser.add_argument("--ignore-user-content", action="store_true")
    parser.add_argument("--ignore-schema-title", action="store_true")
    return parser.parse_args()


def load_official_requests(run_dir: Path, task_id: str) -> list[tuple[Path, dict[str, Any]]]:
    pattern = f"artifacts/task_{task_id}/sim_*/llm_debug/*agent_response*.json" if task_id else "artifacts/task_*/sim_*/llm_debug/*agent_response*.json"
    out: list[tuple[Path, dict[str, Any]]] = []
    for path in sorted(run_dir.glob(pattern)):
        data = read_json(path)
        request = data.get("request")
        if isinstance(request, dict):
            out.append((path, request))
    return out


def load_trpc_requests(run_dir: Path) -> list[tuple[Path, dict[str, Any]]]:
    roots = [run_dir / "trpc_llm_debug", run_dir]
    seen: set[Path] = set()
    out: list[tuple[Path, dict[str, Any]]] = []
    for root in roots:
        for path in sorted(root.glob("*trpc_agent_go_request.json")):
            if path in seen:
                continue
            seen.add(path)
            data = read_json(path)
            request = data.get("request")
            if isinstance(request, dict):
                out.append((path, request))
    return out


def read_json(path: Path) -> dict[str, Any]:
    with path.open(encoding="utf-8") as f:
        return json.load(f)


def normalize_request(
    request: dict[str, Any],
    ignore_user_content: bool,
    ignore_schema_title: bool,
) -> dict[str, Any]:
    kwargs = request.get("kwargs") if isinstance(request.get("kwargs"), dict) else {}
    return drop_none({
        "model": request.get("model"),
        "messages": normalize_messages(request.get("messages") or [], ignore_user_content),
        "tools": normalize_tools(request.get("tools") or [], ignore_schema_title),
        "tool_choice": request.get("tool_choice"),
        "temperature": request.get("temperature", kwargs.get("temperature")),
        "max_tokens": request.get("max_tokens", request.get("max_completion_tokens", kwargs.get("max_tokens"))),
    })


def normalize_messages(messages: list[Any], ignore_user_content: bool) -> list[Any]:
    normalized = []
    for message in messages:
        if not isinstance(message, dict):
            normalized.append(message)
            continue
        content = "<ignored-user-content>" if ignore_user_content and message.get("role") == "user" else normalize_content(message.get("content"))
        item = {
            "role": message.get("role"),
            "content": content,
            "tool_calls": normalize_tool_calls(message.get("tool_calls")),
            "tool_call_id": message.get("tool_call_id"),
        }
        normalized.append(drop_none(item))
    return normalized


def normalize_content(content: Any) -> Any:
    if isinstance(content, list) and all(isinstance(item, str) for item in content):
        return "\n".join(content)
    return content


def normalize_tool_calls(tool_calls: Any) -> Any:
    if not tool_calls:
        return None
    normalized = []
    for call in tool_calls:
        if not isinstance(call, dict):
            normalized.append(call)
            continue
        function = call.get("function") if isinstance(call.get("function"), dict) else {}
        normalized.append(drop_none({
            "id": call.get("id"),
            "type": call.get("type"),
            "function": drop_none({
                "name": function.get("name") or call.get("name"),
                "arguments": normalize_json_string(function.get("arguments")),
            }),
        }))
    return normalized


def normalize_tools(tools: list[Any], ignore_schema_title: bool) -> list[Any]:
    normalized = []
    for tool in tools:
        if not isinstance(tool, dict):
            normalized.append(tool)
            continue
        function = tool.get("function") if isinstance(tool.get("function"), dict) else {}
        normalized.append(drop_none({
            "type": tool.get("type"),
            "function": drop_none({
                "name": function.get("name"),
                "description": function.get("description"),
                "parameters": normalize_schema(function.get("parameters"), ignore_schema_title),
            }),
        }))
    return sorted(normalized, key=lambda item: item.get("function", {}).get("name", ""))


def normalize_schema(value: Any, ignore_schema_title: bool = False) -> Any:
    if isinstance(value, dict):
        return {
            key: normalize_schema(value[key], ignore_schema_title)
            for key in sorted(value)
            if not (ignore_schema_title and key == "title")
        }
    if isinstance(value, list):
        return [normalize_schema(item, ignore_schema_title) for item in value]
    return value


def normalize_json_string(value: Any) -> Any:
    if not isinstance(value, str):
        return value
    try:
        parsed = json.loads(value)
    except json.JSONDecodeError:
        return value
    return normalize_schema(parsed)


def drop_none(value: Any) -> Any:
    if isinstance(value, dict):
        return {key: drop_none(item) for key, item in value.items() if item is not None}
    if isinstance(value, list):
        return [drop_none(item) for item in value]
    return value


def print_diff(left: Any, right: Any) -> None:
    left_text = json.dumps(left, indent=2, sort_keys=True, ensure_ascii=False).splitlines()
    right_text = json.dumps(right, indent=2, sort_keys=True, ensure_ascii=False).splitlines()
    diff = difflib.unified_diff(left_text, right_text, fromfile="official", tofile="trpc", lineterm="")
    sys.stdout.write("\n".join(diff))
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
