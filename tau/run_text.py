#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import threading
from datetime import datetime
from pathlib import Path
from typing import Any, Optional

from adk_python_agent import ADK_AGENT_NAME, register_adk_agent

AGENT_NAME = "trpc_agent_go"
OFFICIAL_AGENT_NAME = "llm_agent"


def main() -> None:
    args = parse_args()
    normalize_openai_env()
    tau2_root = args.tau2_root.resolve()
    bootstrap_tau2(tau2_root)
    if args.agent == AGENT_NAME:
        go_binary = ensure_go_binary(args)
        register_agent(args, go_binary)
    elif args.agent == ADK_AGENT_NAME:
        bootstrap_adk(args.adk_root.resolve())
        register_adk_agent(args)
    run_benchmark(args, tau2_root)


def parse_args() -> argparse.Namespace:
    default_tau2_root = find_default_tau2_root()
    default_adk_root = find_default_adk_root()
    parser = argparse.ArgumentParser(description="Run tau2 text benchmark with an official, ADK Python, or trpc-agent-go LLMAgent.")
    parser.add_argument("--tau2-root", type=Path, default=default_tau2_root)
    parser.add_argument("--adk-root", type=Path, default=default_adk_root)
    parser.add_argument("--agent", default=AGENT_NAME, choices=[AGENT_NAME, ADK_AGENT_NAME, OFFICIAL_AGENT_NAME])
    parser.add_argument("--trpc-planner", default="none", choices=["none", "react"])
    parser.add_argument("--domain", default="retail", choices=["airline", "retail", "telecom"])
    parser.add_argument("--task-split", default="base")
    parser.add_argument("--task-ids", default="")
    parser.add_argument("--num-tasks", type=int, default=2)
    parser.add_argument("--all-tasks", action="store_true")
    parser.add_argument("--num-trials", type=int, default=1)
    parser.add_argument("--max-concurrency", type=int, default=1)
    parser.add_argument("--seed", type=int, default=300)
    parser.add_argument("--agent-model", default=os.getenv("MODEL_NAME", "gpt-4o-mini"))
    parser.add_argument("--agent-temperature", type=float, default=0.0)
    parser.add_argument("--agent-max-tokens", type=int, default=0)
    parser.add_argument("--agent-timeout-seconds", type=int, default=180)
    parser.add_argument("--user-model", default=os.getenv("USER_MODEL", "openai/gpt-4.1-mini"))
    parser.add_argument("--user-temperature", type=float, default=0.0)
    parser.add_argument("--nl-assertion-model", "--nl-assertions-model", dest="nl_assertions_model", default=os.getenv("NL_ASSERTION_MODEL", ""))
    parser.add_argument("--nl-assertions-temperature", type=float, default=0.0)
    parser.add_argument("--max-retries", type=int, default=0)
    parser.add_argument("--retry-delay", type=float, default=1.0)
    parser.add_argument("--output-dir", type=Path, default=Path(__file__).resolve().parent / "results")
    parser.add_argument("--run-name", default="")
    parser.add_argument("--verbose-logs", action="store_true")
    parser.add_argument("--llm-log-mode", default="all", choices=["all", "latest"])
    parser.add_argument("--trpc-llm-log-dir", type=Path, default=None)
    parser.add_argument("--go", default=os.getenv("GO", "go"))
    parser.add_argument("--go-binary", type=Path, default=None)
    parser.add_argument("--no-go-build", action="store_true")
    parser.add_argument("--log-level", default="INFO")
    args = parser.parse_args()
    if args.agent != AGENT_NAME and args.trpc_planner != "none":
        parser.error("--trpc-planner is only supported with --agent trpc_agent_go")
    return args


def find_default_tau2_root() -> Path:
    here = Path(__file__).resolve()
    candidates = [
        here.parents[2] / "tau2-bench",
        here.parents[2] / "tau" / "tau2-bench",
        here.parents[1] / "tau2-bench",
    ]
    return first_existing_path(candidates)


def find_default_adk_root() -> Path:
    here = Path(__file__).resolve()
    candidates = [
        here.parents[4] / "adk-python",
        here.parents[3] / "adk-python",
        here.parents[2] / "adk-python",
        here.parents[1] / "adk-python",
    ]
    return first_existing_path(candidates)


def first_existing_path(candidates: list[Path]) -> Path:
    for candidate in candidates:
        if candidate.exists():
            return candidate
    return candidates[0]


def normalize_openai_env() -> None:
    base_url = os.getenv("OPENAI_BASE_URL", "").strip()
    if base_url and not os.getenv("OPENAI_API_BASE"):
        os.environ["OPENAI_API_BASE"] = base_url


def bootstrap_tau2(tau2_root: Path) -> None:
    src = tau2_root / "src"
    data = tau2_root / "data"
    if not src.exists():
        raise FileNotFoundError(f"tau2 source directory not found: {src}")
    if not data.exists():
        raise FileNotFoundError(f"tau2 data directory not found: {data}")
    sys.path.insert(0, str(src))
    os.environ["TAU2_DATA_DIR"] = str(data)


def bootstrap_adk(adk_root: Path) -> None:
    src = adk_root / "src"
    if src.exists() and str(src) not in sys.path:
        sys.path.insert(0, str(src))


def ensure_go_binary(args: argparse.Namespace) -> Path:
    if args.go_binary is not None:
        return args.go_binary.resolve()
    go_dir = Path(__file__).resolve().parent / "trpc-agent-go-impl"
    bin_dir = args.output_dir.resolve() / "bin"
    binary = bin_dir / "tau-agent-go"
    if args.no_go_build:
        return binary
    bin_dir.mkdir(parents=True, exist_ok=True)
    tmp_binary = bin_dir / f"tau-agent-go.{os.getpid()}.tmp"
    cmd = [args.go, "build", "-o", str(tmp_binary), "."]
    print(f"Building Go LLMAgent adapter: {' '.join(cmd)}")
    try:
        subprocess.run(cmd, cwd=go_dir, check=True)
        os.replace(tmp_binary, binary)
    finally:
        if tmp_binary.exists():
            tmp_binary.unlink()
    return binary


def register_agent(args: argparse.Namespace, go_binary: Path) -> None:
    from tau2.agent.llm_agent import AGENT_INSTRUCTION, SYSTEM_PROMPT
    from tau2.agent.base_agent import HalfDuplexAgent
    from tau2.data_model.message import AssistantMessage, MultiToolMessage, ToolCall
    from tau2.registry import registry
    class TRPCAgentState:
        pass
    class TRPCAgent(HalfDuplexAgent[TRPCAgentState]):
        def __init__(self, tools: list[Any], domain_policy: str, model_name: str, temperature: float):
            super().__init__(tools=tools, domain_policy=domain_policy)
            self.system_prompt = SYSTEM_PROMPT.format(
                domain_policy=domain_policy,
                agent_instruction=AGENT_INSTRUCTION,
            )
            self.tool_schemas = [make_jsonable(tool.openai_schema) for tool in tools]
            self.client = GoLLMAgentClient(
                binary=go_binary,
                model_name=model_name,
                temperature=temperature,
                max_tokens=args.agent_max_tokens,
                timeout_seconds=args.agent_timeout_seconds,
                planner=args.trpc_planner,
                system_prompt=self.system_prompt,
                tools=self.tool_schemas,
            )
        def get_init_state(self, message_history: Optional[list[Any]] = None) -> TRPCAgentState:
            return TRPCAgentState()
        def generate_next_message(self, message: Any, state: TRPCAgentState) -> tuple[Any, TRPCAgentState]:
            incoming = list(message.tool_messages) if isinstance(message, MultiToolMessage) else [message]
            if len(incoming) == 0:
                raise RuntimeError("trpc_agent_go expects at least one input message per turn")
            result = self.client.generate(incoming)
            if result.get("type") == "tool_calls":
                raw_tool_calls = result["tool_calls"]
                tool_calls = [
                    ToolCall(
                        id=call["id"],
                        name=call["name"],
                        arguments=call["arguments"],
                        requestor="assistant",
                    )
                    for call in raw_tool_calls
                ]
                assistant = AssistantMessage(role="assistant", content=None, tool_calls=tool_calls, usage=result.get("usage"))
            else:
                content = result["content"]
                assistant = AssistantMessage(role="assistant", content=content, usage=result.get("usage"))
            return assistant, state
        def stop(self, message: Any = None, state: Optional[TRPCAgentState] = None) -> None:
            self.client.close()
    def create_agent(tools: list[Any], domain_policy: str, **kwargs: Any) -> TRPCAgent:
        llm_args = kwargs["llm_args"]
        model_name = kwargs["llm"]
        temperature = float(llm_args["temperature"])
        return TRPCAgent(tools=tools, domain_policy=domain_policy, model_name=model_name, temperature=temperature)
    registry.register_agent_factory(create_agent, AGENT_NAME)


class GoLLMAgentClient:
    def __init__(
        self,
        binary: Path,
        model_name: str,
        temperature: float,
        max_tokens: int,
        timeout_seconds: int,
        planner: str,
        system_prompt: str,
        tools: list[dict[str, Any]],
    ):
        self.binary = binary
        self.model_name = model_name
        self.temperature = temperature
        self.max_tokens = max_tokens
        self.timeout_seconds = timeout_seconds
        self.planner = planner
        self.lock = threading.RLock()
        self.closed = False
        self.proc = subprocess.Popen(
            [str(self.binary)],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            bufsize=1,
        )
        self._request({
            "type": "start",
            "config": {
                "model": self.model_name,
                "planner": self.planner,
                "system_prompt": system_prompt,
                "tools": tools,
                "max_tokens": self.max_tokens,
                "temperature": self.temperature,
                "timeout_seconds": self.timeout_seconds,
            },
        })
    def generate(self, messages: list[Any]) -> dict[str, Any]:
        return self._request({
            "type": "generate",
            "messages": [dump_message(message) for message in messages],
        })
    def close(self) -> None:
        with self.lock:
            if self.closed:
                return
            self.closed = True
            if self.proc.poll() is None:
                self._request_unlocked({"type": "close"})
                self.proc.wait(timeout=5)
    def _request(self, payload: dict[str, Any]) -> dict[str, Any]:
        with self.lock:
            if self.closed:
                raise RuntimeError("Go LLMAgent adapter is closed")
            return self._request_unlocked(payload)
    def _request_unlocked(self, payload: dict[str, Any]) -> dict[str, Any]:
        if self.proc.stdin is None or self.proc.stdout is None:
            raise RuntimeError("Go LLMAgent adapter pipes are not available")
        self.proc.stdin.write(json.dumps(payload) + "\n")
        self.proc.stdin.flush()
        line = self.proc.stdout.readline()
        if not line:
            stderr = self.proc.stderr.read() if self.proc.poll() is not None and self.proc.stderr else ""
            raise RuntimeError(f"Go LLMAgent adapter stopped without a response. {stderr.strip()}")
        result = json.loads(line)
        if result.get("type") == "error":
            raise RuntimeError(result.get("error", "Go LLMAgent adapter failed"))
        return result


def dump_message(message: Any) -> dict[str, Any]:
    return make_jsonable(message.model_dump(mode="json", exclude_none=True))


def make_jsonable(value: Any) -> Any:
    return json.loads(json.dumps(value))


def run_benchmark(args: argparse.Namespace, tau2_root: Path) -> None:
    from tau2.data_model.simulation import TextRunConfig
    from tau2.metrics.agent_metrics import compute_metrics
    from tau2.runner import get_tasks, run_tasks
    from tau2.utils.llm_utils import set_llm_log_mode
    configure_tau2_evaluator_models(args)
    task_ids = split_csv(args.task_ids)
    num_tasks = selected_num_tasks(args, task_ids)
    tasks = get_tasks(
        task_set_name=args.domain,
        task_split_name=args.task_split,
        task_ids=task_ids,
        num_tasks=num_tasks,
    )
    run_dir = make_run_dir(args)
    configure_trpc_llm_logging(args, run_dir)
    set_llm_log_mode(args.llm_log_mode)
    save_path = run_dir / "results.json"
    config = TextRunConfig(
        domain=args.domain,
        task_split_name=args.task_split,
        task_ids=task_ids,
        num_tasks=num_tasks,
        agent=args.agent,
        llm_agent=args.agent_model,
        llm_args_agent=agent_llm_args(args),
        user="user_simulator",
        llm_user=args.user_model,
        llm_args_user={"temperature": args.user_temperature},
        num_trials=args.num_trials,
        max_concurrency=args.max_concurrency,
        seed=args.seed,
        max_retries=args.max_retries,
        retry_delay=args.retry_delay,
        log_level=args.log_level,
        verbose_logs=args.verbose_logs,
        save_to=run_dir.name,
    )
    print(f"tau2_root={tau2_root}")
    print(f"agent={args.agent} trpc_planner={args.trpc_planner} domain={args.domain} tasks={len(tasks)} output={save_path}")
    results = run_tasks(config, tasks, save_path=save_path, save_dir=run_dir, results_format="json")
    metrics = compute_metrics(results)
    print(json.dumps(metrics.model_dump(mode="json"), indent=2, sort_keys=True))
    print(f"results={save_path}")


def split_csv(value: str) -> Optional[list[str]]:
    items = [item.strip() for item in value.split(",") if item.strip()]
    return items or None


def selected_num_tasks(args: argparse.Namespace, task_ids: Optional[list[str]]) -> Optional[int]:
    if task_ids is not None or args.all_tasks:
        return None
    return args.num_tasks


def make_run_dir(args: argparse.Namespace) -> Path:
    if args.run_name:
        name = args.run_name
    else:
        timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
        model = args.agent_model.replace("/", "_").replace(":", "_")
        name = f"{timestamp}_{args.domain}_{args.agent}_{model}"
        if args.agent == AGENT_NAME and args.trpc_planner != "none":
            name = f"{name}_{args.trpc_planner}"
    run_dir = args.output_dir.resolve() / name
    try:
        run_dir.mkdir(parents=True, exist_ok=False)
    except FileExistsError as exc:
        raise FileExistsError(f"run output directory already exists: {run_dir}") from exc
    return run_dir


def agent_llm_args(args: argparse.Namespace) -> dict[str, Any]:
    llm_args: dict[str, Any] = {"temperature": args.agent_temperature}
    if args.agent_max_tokens > 0:
        llm_args["max_tokens"] = args.agent_max_tokens
    return llm_args


def configure_trpc_llm_logging(args: argparse.Namespace, run_dir: Path) -> None:
    if args.agent != AGENT_NAME:
        os.environ.pop("TAU_TRPC_AGENT_GO_LLM_LOG_DIR", None)
        return
    log_dir = args.trpc_llm_log_dir.resolve() if args.trpc_llm_log_dir else run_dir / "trpc_llm_debug"
    os.environ["TAU_TRPC_AGENT_GO_LLM_LOG_DIR"] = str(log_dir)
    print(f"trpc_llm_debug={log_dir}")


def configure_tau2_evaluator_models(args: argparse.Namespace) -> None:
    if not args.nl_assertions_model:
        return
    import tau2.config as tau2_config
    from tau2.evaluator import evaluator_nl_assertions
    llm_args = {"temperature": args.nl_assertions_temperature}
    tau2_config.DEFAULT_LLM_NL_ASSERTIONS = args.nl_assertions_model
    tau2_config.DEFAULT_LLM_NL_ASSERTIONS_ARGS = llm_args
    evaluator_nl_assertions.DEFAULT_LLM_NL_ASSERTIONS = args.nl_assertions_model
    evaluator_nl_assertions.DEFAULT_LLM_NL_ASSERTIONS_ARGS = llm_args
    print(f"nl_assertions_model={args.nl_assertions_model}")


if __name__ == "__main__":
    main()
