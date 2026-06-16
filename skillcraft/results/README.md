# Evaluation Results — SkillCraft

This directory stores the SkillCraft benchmark artefacts for
`trpc-agent-go`'s `evolution` loop.

The old headline in this folder came from a single historical run. The
current source of truth is the latest **three-run full compare batch**:

- [`full_compare_run1`](full_compare_run1)
- [`full_compare_run2`](full_compare_run2)
- [`full_compare_run3`](full_compare_run3)
- Aggregate JSON:
  [`tools/full_compare_analysis.json`](tools/full_compare_analysis.json)
- Aggregate script:
  [`tools/aggregate_runs.py`](tools/aggregate_runs.py)

## Headline Numbers

Configuration:

- Agent / reviewer model: `gpt-4o-mini`
- Task set: `openmeteo-weather`, `recipe-cookbook-builder`,
  `world-bank-economic-snapshot`
- Difficulty scales: `e1,e2,e3,m1,m2,h1` per family
- Total tasks per run: `18`
- Compare runs: `3`
- Max tool iterations: `24`
- Warm-start seed:
  [`tools/clean_skill_seed`](tools/clean_skill_seed)
- Prompt overview cap: `8`

Aggregate over the three runs:

| Metric | Baseline | Evolution | Δ (Evolution − Baseline) |
| --- | ---: | ---: | ---: |
| Mean pass rate | 90.74% | 90.74% | 0.00pp |
| Pass-rate stddev | 8.49pp | 3.20pp | - |
| Mean end-to-end tokens/task | 169,888.61 | 145,980.13 | -23,908.48 |
| End-to-end token stddev | 81,007.55 | 24,363.25 | 57,153.77 |
| Mean agent tokens/task | 169,888.61 | 131,990.93 | -37,897.68 |
| Mean duration delta | - | - | +21.14s |

Key takeaways:

- Evolution no longer has a clear multi-run pass-rate advantage on this
  benchmark; the three-run mean pass rate is identical to baseline.
- Evolution is more stable in pass-rate variance than baseline.
- Mean end-to-end tokens improved only because one baseline run hit
  catastrophic weather loops. In the other two runs, evolution still
  spent more end-to-end tokens than baseline.
- Evolution exposed skills in every task (`SkillsOffered = 100%`) but
  never invoked `skill_load` in any of the three runs.

## Focused Diagnostics

The current plan specifically tracks `openmeteo-weather/e1,e2,m1`.
Across the three runs:

| Task | Baseline Passes | Evolution Passes | Baseline Mean E2E | Evolution Mean E2E | Main Observation |
| --- | --- | --- | ---: | ---: | --- |
| `openmeteo-weather/e1` | `T,T,T` | `T,T,T` | 489,459.00 | 80,643.67 | one baseline run blew up to 1.32M tokens; evolution stayed much lower |
| `openmeteo-weather/e2` | `T,F,T` | `T,T,T` | 514,047.67 | 189,678.00 | evolution rescued one baseline failure, but still never called `skill_load` |
| `openmeteo-weather/m1` | `T,T,T` | `T,T,T` | 107,458.33 | 215,112.33 | evolution stayed correct but was consistently more expensive |

That changes the old diagnosis:

- the weather loop is no longer a stable deterministic evolution-only bug;
- the current reproducible fact is instead: **explicit skill usage is
  still absent**, and `world-bank-economic-snapshot/e2` is now the only
  evolution failure that reproduces in all three runs.

## Where To Look Next

The latest logs suggest two active issues:

1. `skill_load` remains unused even when relevant skills are offered.
2. `worldbank_*` MCP tools occasionally hit 60-second request timeouts,
   and those timeouts now dominate the recurring failure cluster.

## Directory Layout

```text
results/
|-- README.md
|-- REPORT.md
|-- REPORT.zh_CN.md
|-- tools/
|   |-- extract_metrics.py
|   |-- aggregate_runs.py
|   |-- full_compare_analysis.json
|   |-- metrics.json
|   +-- clean_skill_seed/
|-- full_compare_run1/
|-- full_compare_run2/
|-- full_compare_run3/
+-- ... older historical runs ...
```

## Historical Note

Older artefacts such as [`multi_family_compare`](multi_family_compare)
are still useful for archaeology, but they should no longer be used as
the top-level claim for whether evolution currently helps on SkillCraft.
