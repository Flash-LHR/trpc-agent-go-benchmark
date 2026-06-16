#!/usr/bin/env python3
"""Expose selected SkillCraft local tools as an MCP stdio server."""

from __future__ import annotations

import argparse
import asyncio
import contextlib
import inspect
import io
import json
import os
import sys
from typing import Any


WRITE_FINAL_JSON_TOOL_NAME = "local-write_final_json"


class WriteFinalJSONTool:
    """Reliable final-deliverable writer that does not depend on filesystem MCP."""

    name = WRITE_FINAL_JSON_TOOL_NAME
    description = (
        "Write the final JSON deliverable directly to the workspace. "
        "Validates JSON and auto-recovers common encoding mistakes. "
        "Prefer over write_file for the final task output."
    )
    params_json_schema = {
        "type": "object",
        "properties": {
            "path": {
                "type": "string",
                "description": "Output filename, relative to workspace.",
            },
            "content": {
                "type": "string",
                "description": "Raw JSON text to write.",
            },
        },
        "required": ["path", "content"],
        "additionalProperties": False,
    }


def _resolve_workspace_path(workspace: str, raw_path: str) -> str:
    workspace_abs = os.path.abspath(workspace)
    if not isinstance(raw_path, str) or not raw_path:
        raise ValueError("path must be a non-empty string")
    if os.path.isabs(raw_path):
        full_abs = os.path.abspath(raw_path)
    else:
        full_abs = os.path.abspath(os.path.join(workspace_abs, raw_path))
    if full_abs != workspace_abs and not full_abs.startswith(workspace_abs + os.sep):
        raise ValueError(
            f"path must stay inside the workspace ({workspace_abs}): {raw_path}"
        )
    return full_abs


def _decode_escape_sequences(content: str) -> str:
    """Best-effort recovery for content where JSON escapes were emitted literally."""

    sentinel = "\x00"
    return (
        content.replace("\\\\", sentinel)
        .replace("\\n", "\n")
        .replace("\\r", "\r")
        .replace("\\t", "\t")
        .replace('\\"', '"')
        .replace(sentinel, "\\")
    )


def normalize_json_content(content: str) -> tuple[str, str]:
    """Return (canonical_json_text, info_message).

    Tries direct json.loads first. If that yields a string, it interprets the
    payload as a double-encoded JSON document. If direct parsing fails, attempts
    to decode common literal-escape mistakes (\\n, \\", \\\\) and parse again.
    """

    if not isinstance(content, str):
        raise ValueError("content must be a string")

    try:
        parsed = json.loads(content)
    except json.JSONDecodeError:
        pass
    else:
        if isinstance(parsed, str):
            try:
                inner = json.loads(parsed)
                return (
                    json.dumps(inner, ensure_ascii=False, indent=2),
                    "Decoded a JSON-encoded JSON string before writing.",
                )
            except json.JSONDecodeError:
                # Content was literally a JSON string value; keep verbatim.
                return parsed, "Wrote literal string content."
        return json.dumps(parsed, ensure_ascii=False, indent=2), ""

    unescaped = _decode_escape_sequences(content)
    try:
        parsed = json.loads(unescaped)
    except json.JSONDecodeError as exc:
        raise ValueError(
            f"content is not valid JSON (after escape-recovery attempt): {exc}"
        ) from exc
    return (
        json.dumps(parsed, ensure_ascii=False, indent=2),
        "Recovered JSON by interpreting literal backslash escapes.",
    )


def handle_write_final_json(workspace: str, arguments: dict[str, Any]) -> str:
    if not isinstance(arguments, dict):
        raise ValueError("arguments must be an object with path and content")
    path_value = arguments.get("path")
    content_value = arguments.get("content")
    if path_value is None or content_value is None:
        raise ValueError("both 'path' and 'content' are required")
    full_abs = _resolve_workspace_path(workspace, path_value)
    canonical, info = normalize_json_content(content_value)
    parent = os.path.dirname(full_abs)
    if parent:
        os.makedirs(parent, exist_ok=True)
    with open(full_abs, "w", encoding="utf-8") as handle:
        handle.write(canonical)
    workspace_abs = os.path.abspath(workspace)
    rel = os.path.relpath(full_abs, workspace_abs)
    parts = [f"wrote {len(canonical)} bytes to {rel}"]
    if info:
        parts.append(info)
    return " | ".join(parts)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--skillcraft-root", required=True)
    parser.add_argument("--workspace", required=True)
    parser.add_argument("--toolset", action="append", default=[])
    return parser.parse_args()


def import_skillcraft_tools(skillcraft_root: str):
    skillcraft_root = os.path.abspath(skillcraft_root)
    if skillcraft_root not in sys.path:
        sys.path.insert(0, skillcraft_root)

    captured = io.StringIO()
    try:
        with contextlib.redirect_stdout(captured), contextlib.redirect_stderr(captured):
            from utils.roles.task_agent import local_tool_mappings
            from utils.aux_tools.basic import (
                tool_file_append,
                tool_file_write_json_chunk,
            )
    except Exception as exc:  # pragma: no cover - import errors are fatal
        logs = captured.getvalue().strip()
        message = f"failed to import SkillCraft tool mappings: {exc}"
        if logs:
            message += f"\nCaptured import logs:\n{logs}"
        raise RuntimeError(message) from exc
    return local_tool_mappings, tool_file_append, tool_file_write_json_chunk


def select_tools(
    requested_toolsets: list[str],
    local_tool_mappings: dict[str, Any],
    chunk_tools: list[Any],
) -> list[Any]:
    selected: list[Any] = []

    for toolset_name in requested_toolsets:
        toolset_name = toolset_name.strip()
        if not toolset_name:
            continue
        if toolset_name == "skill_cache":
            # The benchmark uses trpc-agent-go managed skills instead of
            # SkillCraft's native skill cache.
            continue
        if toolset_name not in local_tool_mappings:
            raise ValueError(f"unknown SkillCraft local toolset: {toolset_name}")
        entry = local_tool_mappings[toolset_name]
        if isinstance(entry, list):
            selected.extend(entry)
        else:
            selected.append(entry)

    if "claim_done" in requested_toolsets:
        selected.extend(chunk_tools)
        # Always provide our reliable JSON deliverable writer alongside
        # claim_done so the agent can save its final output even if the
        # filesystem MCP server is misbehaving.
        selected.append(WriteFinalJSONTool())

    deduped = []
    seen_names = set()
    for tool in selected:
        name = getattr(tool, "name", "").strip()
        if not name or name in seen_names:
            continue
        deduped.append(tool)
        seen_names.add(name)
    return deduped


def json_safe(value: Any) -> Any:
    return json.loads(json.dumps(value, ensure_ascii=False, default=str))


def normalize_result(value: Any, types_module) -> Any:
    if isinstance(value, dict):
        return json_safe(value)
    if value is None:
        return [types_module.TextContent(type="text", text="null")]
    if isinstance(value, str):
        return [types_module.TextContent(type="text", text=value)]
    return [
        types_module.TextContent(
            type="text",
            text=json.dumps(json_safe(value), ensure_ascii=False, indent=2),
        )
    ]


async def build_and_run_server(args: argparse.Namespace) -> None:
    local_tool_mappings, tool_file_append, tool_file_write_json_chunk = import_skillcraft_tools(
        args.skillcraft_root
    )

    from agents.tool import RunContextWrapper
    from mcp import types
    from mcp.server import Server
    from mcp.server.stdio import stdio_server

    os.makedirs(args.workspace, exist_ok=True)
    tool_defs = select_tools(
        args.toolset,
        local_tool_mappings,
        [tool_file_append, tool_file_write_json_chunk],
    )
    tool_by_name = {tool.name: tool for tool in tool_defs}
    shared_context = {
        "workspace_path": os.path.abspath(args.workspace),
        "_agent_workspace": os.path.abspath(args.workspace),
        "_claim_done_called": False,
    }

    server = Server(
        name="skillcraft-local-tools",
        instructions="Selected SkillCraft local tools exposed over MCP stdio.",
    )

    @server.list_tools()
    async def list_tools():
        return [
            types.Tool(
                name=tool.name,
                description=getattr(tool, "description", "") or "",
                inputSchema=getattr(tool, "params_json_schema", None) or {"type": "object", "properties": {}},
            )
            for tool in tool_defs
        ]

    @server.call_tool(validate_input=True)
    async def call_tool(tool_name: str, arguments: dict[str, Any]):
        tool = tool_by_name[tool_name]
        if tool_name == WRITE_FINAL_JSON_TOOL_NAME:
            try:
                result = handle_write_final_json(args.workspace, arguments or {})
            except Exception as exc:
                raise RuntimeError(str(exc)) from exc
            return [types.TextContent(type="text", text=result)]

        wrapper = RunContextWrapper(shared_context)
        captured = io.StringIO()
        try:
            with contextlib.redirect_stdout(captured), contextlib.redirect_stderr(captured):
                result = tool.on_invoke_tool(
                    wrapper,
                    json.dumps(arguments or {}, ensure_ascii=False),
                )
                if inspect.isawaitable(result):
                    result = await result
        except Exception as exc:
            logs = captured.getvalue().strip()
            message = str(exc)
            if logs:
                message += f"\nCaptured tool logs:\n{logs[-2000:]}"
            raise RuntimeError(message) from exc
        return normalize_result(result, types)

    async with stdio_server() as (read_stream, write_stream):
        await server.run(
            read_stream,
            write_stream,
            server.create_initialization_options(),
        )


def main() -> None:
    args = parse_args()
    asyncio.run(build_and_run_server(args))


if __name__ == "__main__":
    main()
