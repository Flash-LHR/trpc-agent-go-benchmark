# Tau Benchmark

This directory adds trpc-agent-go and ADK Python adapters for the official `tau2-bench` text benchmark.

The runner reuses the local `../../tau2-bench` checkout for domains, tasks, user simulation, orchestration, tool execution, and evaluation. The tested runtime is responsible for the agent LLM turn: prompt, message history, tool declarations, model response, and tool-call emission.

## Scope

This README tracks two same-workload benchmark runtimes:

- `trpc_agent_go`: trpc-agent-go `LLMAgent`.
- `adk_python`: ADK Python `LlmAgent`.

The full results below use the official tau2 `retail` text domain, all 114 tasks, 4 trials per task, `gpt-5.2` for the tested agent, `openai/gpt-5.2` for the tau2 user simulator, and `openai/gpt-5.2` for tau2 natural-language assertions. No custom `max-steps` or `max-errors` flags are used.

## Layout

- `run_text.py`: Parses benchmark options, bootstraps tau2, registers the selected agent, and runs the text benchmark.
- `adk_python_agent.py`: Registers the `adk_python` tau2 agent and bridges ADK tool calls back to tau2.
- `trpc-agent-go-impl/`: Go adapter process that owns one trpc-agent-go `LLMAgent` runner and in-memory session for a tau2 simulation.
- `compare_llm_requests.py`: Utility for auditing recorded LLM request contexts.
- `results/`: Default output directory for `results.json` and run artifacts.
- `logs/`: Local tee logs for long-running ADK Python runs.

## Prerequisites

Install the official tau2 dependencies from the workspace root:

```bash
cd tau2-bench
uv sync
```

Set model credentials for the tested agent, user simulator, and tau2 evaluators:

```bash
export OPENAI_API_KEY=...
# Optional for OpenAI-compatible endpoints.
export OPENAI_BASE_URL=https://...
```

Go must be available in `PATH` for the trpc-agent-go adapter. In this workspace, `/usr/local/go/bin/go` is a valid explicit value.

ADK Python runs use the local `adk-python` checkout as an editable uv dependency:

```bash
uv run --project ../../tau2-bench \
  --with-editable /cbs/workspace/adk-python \
  python run_text.py --agent adk_python
```

## Quick Smoke Runs

Run from this directory:

```bash
cd trpc-agent-go-benchmark/tau
source ../../.env
```

Small trpc-agent-go `LLMAgent` run:

```bash
uv run --project ../../tau2-bench python run_text.py \
  --agent trpc_agent_go \
  --domain retail \
  --num-tasks 2 \
  --num-trials 1 \
  --max-concurrency 1 \
  --agent-model gpt-4o-mini \
  --user-model openai/gpt-4.1-mini \
  --nl-assertions-model openai/gpt-4.1-mini \
  --go /usr/local/go/bin/go
```

Small ADK Python `LlmAgent` run:

```bash
uv run --project ../../tau2-bench \
  --with-editable /cbs/workspace/adk-python \
  python run_text.py \
  --agent adk_python \
  --domain retail \
  --num-tasks 2 \
  --num-trials 1 \
  --max-concurrency 1 \
  --agent-model gpt-4o-mini \
  --user-model openai/gpt-4.1-mini \
  --nl-assertions-model openai/gpt-4.1-mini
```

Results are written to:

```text
results/<run-name>/results.json
```

## Full Benchmark Runs

Run all retail tasks with trpc-agent-go `LLMAgent`:

```bash
uv run --project ../../tau2-bench python run_text.py \
  --agent trpc_agent_go \
  --domain retail \
  --all-tasks \
  --num-trials 4 \
  --max-concurrency 5 \
  --agent-model gpt-5.2 \
  --user-model openai/gpt-5.2 \
  --nl-assertions-model openai/gpt-5.2 \
  --verbose-logs \
  --llm-log-mode latest \
  --go /usr/local/go/bin/go
```

Run all retail tasks with ADK Python `LlmAgent`:

```bash
uv run --project ../../tau2-bench \
  --with-editable /cbs/workspace/adk-python \
  python run_text.py \
  --agent adk_python \
  --domain retail \
  --all-tasks \
  --num-trials 4 \
  --max-concurrency 5 \
  --agent-model gpt-5.2 \
  --user-model openai/gpt-5.2 \
  --nl-assertions-model openai/gpt-5.2 \
  --verbose-logs \
  --llm-log-mode latest
```

Some retail tasks include tau2 natural-language assertions. Those evaluator calls are separate from both the tested agent and the user simulator. Use `--nl-assertions-model` when tau2's upstream default is not available or when all LiteLLM-side requests should use the same model. `--nl-assertion-model` is also accepted as an alias.

## Results

The following results were produced in this workspace using the full benchmark commands above.

| Runtime | Result file | Average reward | Pass^1 | Pass^2 | Pass^3 | Pass^4 | Infrastructure errors |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| trpc-agent-go `LLMAgent` | `results/20260625_012325_retail_trpc_agent_go_gpt-5.2/results.json` | 67.98% | 67.98% | 53.80% | 46.05% | 41.23% | 0 |
| ADK Python `LlmAgent` | `results/20260701_113244_retail_adk_python_gpt-5.2/results.json` | 72.15% | 72.15% | 59.65% | 52.41% | 47.37% | 0 |

Shared run parameters:

| Parameter | Value |
| --- | --- |
| Domain | `retail` |
| Tasks | 114 |
| Trials per task | 4 |
| Total simulations per runtime | 456 |
| Agent model | `gpt-5.2` |
| User simulator model | `openai/gpt-5.2` |
| NL assertions model | `openai/gpt-5.2` |
| Max concurrency | 5 |

Detailed metrics:

| Metric | trpc-agent-go `LLMAgent` | ADK Python `LlmAgent` |
| --- | ---: | ---: |
| Total simulations | 456 | 456 |
| Scored simulations | 456 | 456 |
| Termination user_stop | 456 | 456 |
| Termination error | 0 | 0 |
| Termination infrastructure_error | 0 | 0 |
| Infrastructure errors | 0 | 0 |
| Average reward | 0.6798245614 | 0.7214912281 |
| Pass^1 | 0.6798245614 | 0.7214912281 |
| Pass^2 | 0.5380116959 | 0.5964912281 |
| Pass^3 | 0.4605263158 | 0.5241228070 |
| Pass^4 | 0.4122807018 | 0.4736842105 |
| DB matches | 317 | 338 |
| DB mismatches | 139 | 118 |
| DB not checked | 0 | 0 |
| Correct read actions | 1332 / 1428 | 1344 / 1428 |
| Correct write actions | 503 / 704 | 527 / 704 |
| Average agent cost | NaN | NaN |

Per-task success count across the four trials:

| Successful trials | trpc-agent-go `LLMAgent` task count | ADK Python `LlmAgent` task count |
| ---: | ---: | ---: |
| 4 / 4 | 47 | 54 |
| 3 / 4 | 22 | 23 |
| 2 / 4 | 20 | 15 |
| 1 / 4 | 16 | 14 |
| 0 / 4 | 9 | 8 |

Artifacts:

| Runtime | Artifact | Path |
| --- | --- | --- |
| trpc-agent-go `LLMAgent` | Result JSON | `results/20260625_012325_retail_trpc_agent_go_gpt-5.2/results.json` |
| ADK Python `LlmAgent` | Result JSON | `results/20260701_113244_retail_adk_python_gpt-5.2/results.json` |
| ADK Python `LlmAgent` | Full run log | `logs/adk_retail_gpt52_full_20260701_113236.log` |
| ADK Python `LlmAgent` | Standalone summary | `results/20260701_113244_retail_adk_python_gpt-5.2/summary.md` |

The ADK Python full-run log was scanned after the process exited:

| Keyword | Count |
| --- | ---: |
| `Task was destroyed` | 0 |
| `Event loop is closed` | 0 |
| `OpenAIException` | 0 |
| `failed permanently after 1 attempts` | 0 |
| `litellm.InternalServerError` | 0 |
| `RateLimitError` | 0 |
| `Infrastructure error` | 0 |
| `Traceback` | 0 |

Cost-accounting warnings such as `Message assistant ... has no cost` can appear in tau2 logs. They are tau2 cost metadata warnings and do not correspond to API, LiteLLM, or infrastructure failures.

## Runtime Wiring

Both adapters keep the official tau2 environment as the source of truth:

- The tau2 runner selects the task, user simulator, environment, domain policy prompt, and task-specific tools.
- The tested runtime receives the tau2 prompt/messages/tools and returns either assistant text or model tool calls.
- tau2 executes the domain tools, mutates the official environment state, records action traces, and computes DB/action/NL assertion rewards.

trpc-agent-go `LLMAgent` path:

- Prompt: `run_text.py` imports tau2's `AGENT_INSTRUCTION` and `SYSTEM_PROMPT`, renders the domain policy prompt, and passes it to `llmagent.WithInstruction`.
- Messages: the Python adapter starts one Go process per tau2 agent. The Go process owns one `runner.Runner` and one in-memory session. Each tau2 turn calls `runner.Run` with the latest user or tool message.
- Tools: tau2 `environment.get_tools()` provides task-specific tools. The Go side converts those schemas into trpc-agent-go `tool.Declaration` values and passes them with `agent.WithExternalTools`.
- Execution: when the model emits tool calls, trpc-agent-go returns the assistant tool-call response to tau2. The next tau2 turn passes the real tau2 tool results back through the same session.

ADK Python `LlmAgent` path:

- Prompt: the adapter uses the same rendered tau2 domain policy prompt as the ADK instruction.
- Messages: the adapter keeps one ADK session per tau2 simulation and forwards each tau2 user or tool-result turn into that session.
- Tools: tau2 task-specific tool schemas are exposed as ADK tools, while actual tool execution remains in tau2.
- Execution: ADK emits assistant text or tool calls; tau2 executes those tool calls and evaluates the resulting state.

## Useful Options

Run specific tasks:

```bash
uv run --project ../../tau2-bench python run_text.py \
  --agent trpc_agent_go \
  --domain retail \
  --task-ids 0,1 \
  --go /usr/local/go/bin/go
```

Reuse an already built Go adapter:

```bash
uv run --project ../../tau2-bench python run_text.py \
  --agent trpc_agent_go \
  --domain retail \
  --task-ids 0 \
  --no-go-build
```

Record all agent-side LLM request contexts for inspection:

```bash
uv run --project ../../tau2-bench python run_text.py \
  --agent trpc_agent_go \
  --domain retail \
  --task-ids 0 \
  --run-name inspect_trpc_task0 \
  --verbose-logs \
  --llm-log-mode all \
  --go /usr/local/go/bin/go
```

When `--verbose-logs` is enabled, official tau2 request logs are written under each task artifact's `llm_debug` directory. trpc-agent-go raw request logs are written to `results/<run-name>/trpc_llm_debug`.
