#!/usr/bin/env python3
from __future__ import annotations

import asyncio
import atexit
import json
import os
import threading
import time
import uuid
from contextlib import aclosing
from typing import Any


ADK_AGENT_NAME = "adk_python"


class ADKEventLoopBridge:
    def __init__(self):
        self.loop = asyncio.new_event_loop()
        self.thread = threading.Thread(target=self._run_loop, name="tau-adk-event-loop", daemon=True)
        self.thread.start()
        atexit.register(self.close)
    def _run_loop(self) -> None:
        asyncio.set_event_loop(self.loop)
        self.loop.run_forever()
    def run(self, coroutine: Any) -> Any:
        return asyncio.run_coroutine_threadsafe(coroutine, self.loop).result()
    def close(self) -> None:
        self.run(self._cancel_pending_tasks())
        self.loop.call_soon_threadsafe(self.loop.stop)
        self.thread.join()
        self.loop.close()
    async def _cancel_pending_tasks(self) -> None:
        tasks = [task for task in asyncio.all_tasks() if task is not asyncio.current_task()]
        for task in tasks:
            task.cancel()
        await asyncio.gather(*tasks, return_exceptions=True)
        await self.loop.shutdown_asyncgens()


adk_event_loop_bridge: ADKEventLoopBridge | None = None
adk_event_loop_bridge_lock = threading.Lock()


def get_adk_event_loop_bridge() -> ADKEventLoopBridge:
    global adk_event_loop_bridge
    with adk_event_loop_bridge_lock:
        if adk_event_loop_bridge is None:
            adk_event_loop_bridge = ADKEventLoopBridge()
        return adk_event_loop_bridge


def register_adk_agent(args: Any) -> None:
    from google.adk.agents import LlmAgent
    from google.adk.models.lite_llm import LiteLlm
    from google.adk.runners import Runner
    from google.adk.sessions.in_memory_session_service import InMemorySessionService
    from google.adk.tools.base_tool import BaseTool
    from google.genai import types
    from tau2.agent.base_agent import HalfDuplexAgent
    from tau2.agent.llm_agent import AGENT_INSTRUCTION, SYSTEM_PROMPT
    from tau2.data_model.message import AssistantMessage, MultiToolMessage, ToolCall
    from tau2.registry import registry
    ExternalTauTool = make_external_tau_tool_type(BaseTool, types)
    class ADKAgentState:
        pass
    class ADKAgent(HalfDuplexAgent[ADKAgentState]):
        def __init__(self, tools: list[Any], domain_policy: str, model_name: str, temperature: float):
            super().__init__(tools=tools, domain_policy=domain_policy)
            self.system_prompt = SYSTEM_PROMPT.format(
                domain_policy=domain_policy,
                agent_instruction=AGENT_INSTRUCTION,
            )
            self.client = ADKLLMAgentClient(
                model_name=model_name,
                temperature=temperature,
                max_tokens=args.agent_max_tokens,
                timeout_seconds=args.agent_timeout_seconds,
                system_prompt=self.system_prompt,
                tools=[ExternalTauTool(make_jsonable(tool.openai_schema)) for tool in tools],
                llm_agent_cls=LlmAgent,
                lite_llm_cls=LiteLlm,
                runner_cls=Runner,
                session_service_cls=InMemorySessionService,
                genai_types=types,
            )
        def get_init_state(self, message_history: list[Any] | None = None) -> ADKAgentState:
            return ADKAgentState()
        def generate_next_message(self, message: Any, state: ADKAgentState) -> tuple[Any, ADKAgentState]:
            incoming = list(message.tool_messages) if isinstance(message, MultiToolMessage) else message
            result = self.client.generate(incoming)
            if result["type"] == "tool_calls":
                tool_calls = [
                    ToolCall(
                        id=call["id"],
                        name=call["name"],
                        arguments=call["arguments"],
                        requestor="assistant",
                    )
                    for call in result["tool_calls"]
                ]
                assistant = AssistantMessage(role="assistant", content=None, tool_calls=tool_calls, usage=result.get("usage"))
            else:
                assistant = AssistantMessage(role="assistant", content=result["content"], usage=result.get("usage"))
            return assistant, state
        def stop(self, message: Any = None, state: ADKAgentState | None = None) -> None:
            self.client.close()
    def create_agent(tools: list[Any], domain_policy: str, **kwargs: Any) -> ADKAgent:
        llm_args = kwargs["llm_args"]
        model_name = kwargs["llm"]
        temperature = float(llm_args["temperature"])
        return ADKAgent(tools=tools, domain_policy=domain_policy, model_name=model_name, temperature=temperature)
    registry.register_agent_factory(create_agent, ADK_AGENT_NAME)


def make_external_tau_tool_type(base_tool: Any, genai_types: Any) -> type[Any]:
    class ExternalTauTool(base_tool):
        def __init__(self, openai_schema: dict[str, Any]):
            function_schema = openai_schema["function"]
            self._declaration = genai_types.FunctionDeclaration(
                name=function_schema["name"],
                description=function_schema.get("description", ""),
                parameters=genai_types.Schema.model_validate(
                    normalize_json_schema_for_adk(function_schema.get("parameters") or {})
                ),
            )
            super().__init__(name=self._declaration.name, description=self._declaration.description or "")
        def _get_declaration(self) -> Any:
            return self._declaration
        async def run_async(self, *, args: dict[str, Any], tool_context: Any) -> Any:
            raise RuntimeError("Tau2 executes benchmark tools outside ADK.")
    return ExternalTauTool


class ADKLLMAgentClient:
    def __init__(
        self,
        model_name: str,
        temperature: float,
        max_tokens: int,
        timeout_seconds: int,
        system_prompt: str,
        tools: list[Any],
        llm_agent_cls: Any,
        lite_llm_cls: Any,
        runner_cls: Any,
        session_service_cls: Any,
        genai_types: Any,
    ):
        self.app_name = "tau_adk_python"
        self.user_id = "tau-user"
        self.session_id = str(uuid.uuid4())
        self.tool_call_names: dict[str, str] = {}
        self.genai_types = genai_types
        self.event_loop_bridge = get_adk_event_loop_bridge()
        model_kwargs: dict[str, Any] = {"timeout": timeout_seconds}
        if os.getenv("OPENAI_BASE_URL", "").strip():
            model_kwargs["api_base"] = os.getenv("OPENAI_BASE_URL", "").strip()
        generate_config: dict[str, Any] = {"temperature": temperature}
        if max_tokens > 0:
            generate_config["max_output_tokens"] = max_tokens
        self.session_service = session_service_cls()
        self.agent = llm_agent_cls(
            name="tau_benchmark_agent",
            model=lite_llm_cls(model=model_name, **model_kwargs),
            instruction=system_prompt,
            tools=tools,
            generate_content_config=genai_types.GenerateContentConfig(**generate_config),
        )
        self.runner = runner_cls(
            app_name=self.app_name,
            agent=self.agent,
            session_service=self.session_service,
        )
        self.event_loop_bridge.run(self.session_service.create_session(
            app_name=self.app_name,
            user_id=self.user_id,
            session_id=self.session_id,
        ))
    def generate(self, message: Any) -> dict[str, Any]:
        return self.event_loop_bridge.run(self._generate_async(message))
    async def _generate_async(self, message: Any) -> dict[str, Any]:
        start = time.perf_counter()
        usage = None
        text_parts: list[str] = []
        new_message = self._content_from_tau(message)
        async with aclosing(self.runner.run_async(
            user_id=self.user_id,
            session_id=self.session_id,
            new_message=new_message,
        )) as events:
            async for event in events:
                if event.error_code:
                    raise RuntimeError(event.error_message or str(event.error_code))
                usage = usage_from_adk(event.usage_metadata) or usage
                calls = event.get_function_calls()
                if calls:
                    tool_calls = [self._tool_call_from_adk(call) for call in calls]
                    return {"type": "tool_calls", "tool_calls": tool_calls, "usage": usage, "generation_time_seconds": time.perf_counter() - start}
                text_parts.extend(text_parts_from_event(event))
        return {"type": "text", "content": "".join(text_parts).strip(), "usage": usage, "generation_time_seconds": time.perf_counter() - start}
    def _tool_call_from_adk(self, call: Any) -> dict[str, Any]:
        call_id = call.id or f"adk-call-{len(self.tool_call_names) + 1}"
        name = call.name or ""
        self.tool_call_names[call_id] = name
        return {"id": call_id, "name": name, "arguments": call.args or {}}
    def _content_from_tau(self, message: Any) -> Any:
        if isinstance(message, list):
            return self.genai_types.Content(
                role="user",
                parts=[self._function_response_part(item) for item in message],
            )
        if message.role == "tool":
            return self.genai_types.Content(role="user", parts=[self._function_response_part(message)])
        return self.genai_types.Content(role="user", parts=[self.genai_types.Part.from_text(text=message.content or "")])
    def _function_response_part(self, message: Any) -> Any:
        name = self.tool_call_names.get(message.id, "")
        response = {"result": message.content or ""}
        part = self.genai_types.Part.from_function_response(name=name, response=response)
        part.function_response.id = message.id
        return part
    def close(self) -> None:
        return None


def normalize_json_schema_for_adk(schema: dict[str, Any]) -> dict[str, Any]:
    defs = schema.get("$defs") or schema.get("defs") or {}
    normalized = normalize_json_schema_node(schema, defs)
    if not normalized:
        return {"type": "object", "properties": {}}
    return normalized


def normalize_json_schema_node(value: Any, defs: dict[str, Any]) -> Any:
    if isinstance(value, list):
        return [normalize_json_schema_node(item, defs) for item in value]
    if not isinstance(value, dict):
        return value
    if "$ref" in value or "ref" in value:
        ref = value.get("$ref") or value.get("ref")
        resolved = resolve_local_json_ref(ref, defs)
        merged = {k: v for k, v in value.items() if k not in {"$ref", "ref"}}
        base = normalize_json_schema_node(resolved, defs)
        if isinstance(base, dict):
            base.update(normalize_json_schema_node(merged, defs))
        return base
    obj = {}
    for key, item in value.items():
        if key in {"$defs", "defs", "$schema"}:
            continue
        normalized_key = normalize_json_schema_key(key)
        obj[normalized_key] = normalize_json_schema_node(item, defs)
    return normalize_nullable_schema(obj)


def resolve_local_json_ref(ref: str, defs: dict[str, Any]) -> dict[str, Any]:
    prefix = "#/$defs/"
    alt_prefix = "#/defs/"
    if ref.startswith(prefix):
        name = ref[len(prefix):]
    elif ref.startswith(alt_prefix):
        name = ref[len(alt_prefix):]
    else:
        raise ValueError(f"Unsupported JSON schema ref: {ref}")
    if name not in defs:
        raise ValueError(f"JSON schema ref not found: {ref}")
    return defs[name]


def normalize_json_schema_key(key: str) -> str:
    key_map = {
        "additionalProperties": "additional_properties",
        "anyOf": "any_of",
        "maxItems": "max_items",
        "maxLength": "max_length",
        "maxProperties": "max_properties",
        "minItems": "min_items",
        "minLength": "min_length",
        "minProperties": "min_properties",
        "propertyOrdering": "property_ordering",
    }
    return key_map.get(key, key)


def normalize_nullable_schema(obj: dict[str, Any]) -> dict[str, Any]:
    type_value = obj.get("type")
    if isinstance(type_value, list):
        non_null = [item for item in type_value if item != "null"]
        if len(non_null) == 1:
            obj["type"] = non_null[0]
            obj["nullable"] = True
    any_of = obj.get("any_of")
    if isinstance(any_of, list):
        non_null_options = [
            option for option in any_of
            if not (isinstance(option, dict) and option.get("type") == "null")
        ]
        if len(non_null_options) == 1 and len(non_null_options) != len(any_of):
            merged = dict(non_null_options[0])
            for key, value in obj.items():
                if key != "any_of" and key not in merged:
                    merged[key] = value
            merged["nullable"] = True
            return merged
    return obj


def usage_from_adk(usage: Any) -> dict[str, int] | None:
    if usage is None:
        return None
    prompt = int(getattr(usage, "prompt_token_count", 0) or 0)
    completion = int(getattr(usage, "candidates_token_count", 0) or 0)
    total = int(getattr(usage, "total_token_count", 0) or prompt + completion)
    return {
        "prompt_tokens": prompt,
        "completion_tokens": completion,
        "total_tokens": total,
    }


def text_parts_from_event(event: Any) -> list[str]:
    if not event.content or not event.content.parts:
        return []
    return [part.text for part in event.content.parts if part.text]


def make_jsonable(value: Any) -> Any:
    return json.loads(json.dumps(value))
