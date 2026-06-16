# SkillCraft Benchmark for trpc-agent-go

This benchmark runs local [SkillCraft](https://github.com/shiqichen17/SkillCraft)
tasks with `trpc-agent-go` and compares:

- `baseline`: no learned skills.
- `evolution`: reuse skills extracted asynchronously by the `evolution`
  reviewer between tasks.
- `compare`: run `baseline` + `evolution` back-to-back and report deltas.

`trpc-agent-go` intentionally keeps skill management on the
reviewer-driven async path. The old in-flow `skill_manage` experiment
was removed after early runs showed prompt overhead without reliable
tool usage.

## Latest Snapshot

The current sources of truth are the **v19 three-run batch** (answers
"does evolution help at all?") and the **v20 three-run batch**
(answers "can we trust it under a quality gate?"). See
[`results/REPORT.md`](results/REPORT.md) (English) and
[`results/REPORT.zh_CN.md`](results/REPORT.zh_CN.md) (中文) for the
full write-up.

### v19 headline (runtime changes, no quality gate)

- [`results/multi_family_compare_v19_try1`](results/multi_family_compare_v19_try1)
- [`results/multi_family_compare_v19_try2`](results/multi_family_compare_v19_try2)
- [`results/multi_family_compare_v19_try3`](results/multi_family_compare_v19_try3)

Across these three runs:

- Baseline mean pass rate: `96.30%`. Evolution mean pass rate: `91.67%`.
- Baseline mean end-to-end tokens / task: `156,091`.
- Evolution mean end-to-end tokens / task: `132,584`
  (**-23,507, -15.1%**).
- Baseline end-to-end token stddev: `58,517`. Evolution stddev:
  `14,109` (**~24% of baseline variance**).
- `skill_load` invocation rate: baseline `0%`, evolution **`100%`**
  across all three runs.
- One evolution "win" rescues a 1.2M-token catastrophic baseline loop
  on `weather/e1` in try2.

### v20 headline (quality gate Phase A + B live)

- [`results/multi_family_compare_v20_try1`](results/multi_family_compare_v20_try1)
- [`results/multi_family_compare_v20_try2`](results/multi_family_compare_v20_try2)
- [`results/multi_family_compare_v20_try3`](results/multi_family_compare_v20_try3)
- [`results/multi_family_compare_v20_try4`](results/multi_family_compare_v20_try4)
- [`results/multi_family_compare_v20_try5`](results/multi_family_compare_v20_try5)

Across these five runs:

- Baseline mean pass rate: `95.00%`. Evolution mean pass rate:
  **`98.33%`** (**+3.33pp**).
- Baseline mean end-to-end tokens / task: `148,396`. Evolution:
  `129,408` (**-18,988, -12.8%**).
- Baseline e2e-token stddev: `84,820`. Evolution e2e-token stddev:
  `14,857` (**17.5% of baseline variance**).
- `skill_load` invocation rate: **still 100%** across all five runs.
- 47 revisions written and promoted across the five runs combined.
- 0 spec-gate rejections, 0 safety-gate rejections.
- Every promoted skill has an on-disk `managed_skills_revisions/<id>/`
  directory with immutable revisions, an append-only `audit.log`, and
  an `active.txt` rollback pointer.

Read the first three runs in isolation and you would conclude v20
slightly regressed; `try4` (baseline's `weather/e1` at 1.34M tokens
+ `agent_error`, `weather/m1` at 1.10M tokens, `recipe/e3` failed,
evolution 12/12 at 119k tokens each) flips the aggregate and
reproduces v19's pattern — evolution's benefit shows up on the
runs where baseline catastrophically loops. Effectiveness
evaluation in Phase C should therefore always use **five-run
aggregates with stddev**, never single runs or three-run means.

### Historical reference: v18 three-run batch

- [`results/full_compare_run1`](results/full_compare_run1)
- [`results/full_compare_run2`](results/full_compare_run2)
- [`results/full_compare_run3`](results/full_compare_run3)
- Aggregate JSON:
  [`results/tools/full_compare_analysis.json`](results/tools/full_compare_analysis.json)
- Aggregate script:
  [`results/tools/aggregate_runs.py`](results/tools/aggregate_runs.py)

v18 was the plateau where `skill_load` never fired and mean pass rate
tied; kept here as historical baseline only.

## Reproducing The Current Batches

From `skillcraft/trpc-agent-go-impl`:

### v19 (quality gate off)

```bash
go run . \
  -skillcraft-root "$SKILLCRAFT_ROOT" \
  -tasks "openmeteo-weather/e1,openmeteo-weather/e2,openmeteo-weather/e3,openmeteo-weather/m1,openmeteo-weather/m2,openmeteo-weather/h1,recipe-cookbook-builder/e1,recipe-cookbook-builder/e2,recipe-cookbook-builder/e3,recipe-cookbook-builder/m1,recipe-cookbook-builder/m2,recipe-cookbook-builder/h1" \
  -mode compare \
  -model gpt-4o-mini \
  -reviewer-model gpt-4o-mini \
  -max-tool-iterations 24 \
  -load-skills-from ../results/tools/seed_skills \
  -max-prompt-skills 8 \
  -output ../results/multi_family_compare_v19_tryN
```

### v20 (quality gate on)

```bash
go run . \
  -skillcraft-root "$SKILLCRAFT_ROOT" \
  -tasks "openmeteo-weather/e1,openmeteo-weather/e2,openmeteo-weather/e3,openmeteo-weather/m1,openmeteo-weather/m2,openmeteo-weather/h1,recipe-cookbook-builder/e1,recipe-cookbook-builder/e2,recipe-cookbook-builder/e3,recipe-cookbook-builder/m1,recipe-cookbook-builder/m2,recipe-cookbook-builder/h1" \
  -mode compare \
  -model gpt-4o-mini \
  -reviewer-model gpt-4o-mini \
  -max-tool-iterations 24 \
  -load-skills-from ../results/tools/seed_skills \
  -max-prompt-skills 8 \
  -enable-approval-gate \
  -output ../results/multi_family_compare_v20_tryN
```

Run each command three times with distinct output directories to
reproduce the three-run batch. The warm-start seed used by all runs
lives at [`results/tools/seed_skills`](results/tools/seed_skills)
(9 generic-parent-only skills covering all 5 task families).

The benchmark impl's `go.mod` uses
`replace trpc.group/trpc-go/trpc-agent-go => /workspace/github/my-trpc-agent-go`
so library-side changes (the top-skill hint in
`internal/flow/processor/skills.go`, the Phase A + B revision store
and gates in `evolution/`) are picked up directly.

## What It Does

For each selected SkillCraft task, the runner:

1. Creates a clean task workspace and copies `initial_workspace/` if present.
2. Exposes SkillCraft local Python tools through a small MCP stdio bridge.
3. Exposes workspace file operations through the standard filesystem MCP server.
4. Runs the task with `trpc-agent-go`.
5. Invokes SkillCraft's native `evaluation/main.py` to get the official score JSON.
6. In `evolution` mode, stores learned `SKILL.md` files and makes them available to later tasks.

The MCP bridge also ships `local-write_final_json`, which writes the
final JSON deliverable directly to the workspace and recovers from
common encoding mistakes. Prompts steer the agent to prefer this tool
for the final deliverable.

## Key Flags

| Flag | Description |
|------|-------------|
| `-skillcraft-root` | Local SkillCraft checkout path |
| `-tasks` | Explicit task directories, comma-separated |
| `-base-task` | Base task family, e.g. `cat-facts-collector` |
| `-scales` | Scale list for `-base-task`, e.g. `e1,e2,e3` |
| `-mode` | `baseline`, `evolution`, or `compare` |
| `-model` | Agent model |
| `-reviewer-model` | Evolution reviewer model |
| `-output` | Result directory |
| `-task-timeout-seconds` | Override task timeout |
| `-max-tool-iterations` | Max tool loops per task |
| `-load-skills-from` | Warm-start the evolution arm from an existing managed-skill directory |
| `-max-prompt-skills` | Cap the number of full skill summaries rendered into the prompt |
| `-enable-approval-gate` | Route reviewer output through the Phase A revision store + Phase B deterministic SpecGate / SafetyGate; writes immutable revisions and an audit log under `<output>/managed_skills_revisions/` |
| `-approval-gate-shadow` | Run the quality gate in shadow mode: still publish even when gates reject, for comparison only |

## Output Layout

The output directory contains:

- `results.json`: structured benchmark results (each task's
  `approvalGate` snapshot is recorded here when `-enable-approval-gate`
  is set).
- `REPORT.md`: readable single-run summary.
- `workspaces/`: per-task workspaces when `-keep-workspaces=true`.
- `managed_skills/`: learned skills produced during the `evolution` arm
  (this is what agents actually read).
- `managed_skills_revisions/`: Phase A revision store, only populated
  when `-enable-approval-gate` is set. Each SkillID gets
  `revisions/<revision-id>/{meta.json, SKILL.md}`, an append-only
  `audit.log`, and an `active.txt` pointer. Rolling back a skill is a
  one-line edit to `active.txt`.

## Requirements

- A working SkillCraft checkout with `uv` dependencies available
- `uv` on `PATH`
- `npx` on `PATH` for `@modelcontextprotocol/server-filesystem`
- Network access for the public APIs used by the selected SkillCraft tasks
- Model credentials exposed as OpenAI-compatible environment variables
