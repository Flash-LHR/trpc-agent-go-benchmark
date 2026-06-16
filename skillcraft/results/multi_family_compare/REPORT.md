# SkillCraft Benchmark Report

- Time: `2026-04-17T16:40:53+08:00`
- Requested mode: `compare`
- Model: `gpt-4o-mini`
- Reviewer model: `gpt-4o-mini`

## Task Set

- `openmeteo-weather/e1`: Weather monitoring with 3 cities × 3 APIs (easy)
- `openmeteo-weather/e2`: Weather monitoring with 3 cities × 3 APIs (easy)
- `openmeteo-weather/e3`: Weather monitoring with 3 cities × 3 APIs (easy)
- `openmeteo-weather/m1`: Weather monitoring with 4 cities × 4 APIs (medium)
- `openmeteo-weather/m2`: Weather monitoring with 4 cities × 4 APIs (medium)
- `openmeteo-weather/h1`: Weather monitoring with 5 cities × 5 APIs (hard)
- `recipe-cookbook-builder/e1`: Recipe cookbook with 3 dishes × 3 APIs (easy)
- `recipe-cookbook-builder/e2`: Recipe cookbook with 3 dishes × 3 APIs (easy)
- `recipe-cookbook-builder/e3`: Recipe cookbook with 3 dishes × 3 APIs (easy)
- `recipe-cookbook-builder/m1`: Recipe cookbook with 4 dishes × 4 APIs (medium)
- `recipe-cookbook-builder/m2`: Recipe cookbook with 4 dishes × 4 APIs (medium)
- `recipe-cookbook-builder/h1`: Recipe cookbook with 5 dishes × 5 APIs (hard)
- `world-bank-economic-snapshot/e1`: Economic snapshot with 3 countries × 3 APIs (easy)
- `world-bank-economic-snapshot/e2`: Economic snapshot with 3 countries × 3 APIs (easy)
- `world-bank-economic-snapshot/e3`: Economic snapshot with 3 countries × 3 APIs (easy)
- `world-bank-economic-snapshot/m1`: Economic snapshot with 4 countries × 4 APIs (medium)
- `world-bank-economic-snapshot/m2`: Economic snapshot with 4 countries × 4 APIs (medium)
- `world-bank-economic-snapshot/h1`: Economic snapshot with 5 countries × 5 APIs (hard)

## Baseline

- Tasks: 18
- Passed tasks: 15
- Pass rate: 83.33%
- Average score: 80.46%
- Average duration: 98.93s
- Average agent tokens: 185590.44
- Average reviewer tokens: 0.00
- Average end-to-end tokens: 185590.44
- Claim-done rate: 77.78%
- Skill-usage-observed rate: 0.00%
- Cold-start subset: 18 tasks, 80.46% score, 185590.44 agent tokens

| Task | Status | Score | Agent Tokens | End-to-end Tokens | Claim Done | Skill Used |
|------|--------|------:|-------------:|------------------:|-----------:|-----------:|
| `openmeteo-weather/e1` | `agent_error` | 0.0 | 714167 | 714167 | no | no |
| `openmeteo-weather/e2` | `ok` | 100.0 | 72641 | 72641 | yes | no |
| `openmeteo-weather/e3` | `ok` | 95.0 | 72278 | 72278 | yes | no |
| `openmeteo-weather/m1` | `ok` | 100.0 | 101619 | 101619 | yes | no |
| `openmeteo-weather/m2` | `ok` | 96.0 | 131287 | 131287 | yes | no |
| `openmeteo-weather/h1` | `ok` | 96.7 | 172117 | 172117 | yes | no |
| `recipe-cookbook-builder/e1` | `ok` | 94.3 | 99147 | 99147 | yes | no |
| `recipe-cookbook-builder/e2` | `ok` | 91.7 | 305005 | 305005 | no | no |
| `recipe-cookbook-builder/e3` | `ok` | 91.7 | 116444 | 116444 | yes | no |
| `recipe-cookbook-builder/m1` | `ok` | 94.3 | 88445 | 88445 | yes | no |
| `recipe-cookbook-builder/m2` | `ok` | 94.3 | 118146 | 118146 | yes | no |
| `recipe-cookbook-builder/h1` | `ok` | 94.3 | 324197 | 324197 | yes | no |
| `world-bank-economic-snapshot/e1` | `ok` | 100.0 | 67222 | 67222 | yes | no |
| `world-bank-economic-snapshot/e2` | `agent_error` | 0.0 | 307614 | 307614 | no | no |
| `world-bank-economic-snapshot/e3` | `ok` | 100.0 | 49284 | 49284 | yes | no |
| `world-bank-economic-snapshot/m1` | `ok` | 100.0 | 124263 | 124263 | yes | no |
| `world-bank-economic-snapshot/m2` | `agent_error` | 0.0 | 350403 | 350403 | no | no |
| `world-bank-economic-snapshot/h1` | `ok` | 100.0 | 126349 | 126349 | yes | no |

## Evolution

- Tasks: 18
- Passed tasks: 18
- Pass rate: 100.00%
- Average score: 97.68%
- Average duration: 79.68s
- Average agent tokens: 118670.06
- Average reviewer tokens: 10243.17
- Average end-to-end tokens: 128913.22
- Claim-done rate: 100.00%
- Skill-usage-observed rate: 0.00%
- Learned skills: `Collect Weather Data for Five Cities with Historical Data`, `Collect Weather Data for Four Cities with Historical Data`, `Collect Weather Data for Four Cities with Summary Statistics`, `Collect Weather Data for Multiple Cities`, `Collect Weather Data for Three Cities with Historical Data`, `Collect Weather Data for Three Cities with Summary Statistics`, `Create Cookbook for Four International Dishes`, `Create Economic Snapshot for Five Countries`, `Create Economic Snapshot for Four Countries`, `Create Economic Snapshot for Three Countries`, `Create Economic Snapshots for Multiple Countries`, `Create Recipe Cookbook for Five International Dishes`, `Create Recipe Cookbook for Four International Dishes`, `Create Recipe Cookbook with 3 International Dishes`, `Create Recipe Cookbook with International Dishes`, `Create Recipe Cookbook with Specific Dishes`
- Warm-start subset: 17 tasks, 97.55% score, 120653.06 agent tokens
- Cold-start subset: 1 tasks, 100.00% score, 84959.00 agent tokens

| Task | Status | Score | Agent Tokens | End-to-end Tokens | Claim Done | Skill Used |
|------|--------|------:|-------------:|------------------:|-----------:|-----------:|
| `openmeteo-weather/e1` | `ok` | 100.0 | 84959 | 93252 | yes | no |
| `openmeteo-weather/e2` | `ok` | 100.0 | 77651 | 84303 | yes | no |
| `openmeteo-weather/e3` | `ok` | 95.0 | 100107 | 107847 | yes | no |
| `openmeteo-weather/m1` | `ok` | 100.0 | 107369 | 117403 | yes | no |
| `openmeteo-weather/m2` | `ok` | 96.0 | 139386 | 151213 | yes | no |
| `openmeteo-weather/h1` | `ok` | 96.7 | 125557 | 137862 | yes | no |
| `recipe-cookbook-builder/e1` | `ok` | 94.3 | 72017 | 78887 | yes | no |
| `recipe-cookbook-builder/e2` | `ok` | 96.7 | 69156 | 75583 | yes | no |
| `recipe-cookbook-builder/e3` | `ok` | 96.7 | 79573 | 86592 | yes | no |
| `recipe-cookbook-builder/m1` | `ok` | 94.3 | 132049 | 143924 | yes | no |
| `recipe-cookbook-builder/m2` | `ok` | 94.3 | 174443 | 188838 | yes | no |
| `recipe-cookbook-builder/h1` | `ok` | 94.3 | 213779 | 231553 | yes | no |
| `world-bank-economic-snapshot/e1` | `ok` | 100.0 | 77008 | 82923 | yes | no |
| `world-bank-economic-snapshot/e2` | `ok` | 100.0 | 101499 | 109841 | yes | no |
| `world-bank-economic-snapshot/e3` | `ok` | 100.0 | 92493 | 101335 | yes | no |
| `world-bank-economic-snapshot/m1` | `ok` | 100.0 | 110672 | 119152 | yes | no |
| `world-bank-economic-snapshot/m2` | `ok` | 100.0 | 129368 | 142637 | yes | no |
| `world-bank-economic-snapshot/h1` | `ok` | 100.0 | 248975 | 267293 | yes | no |

## Comparison

- Overall pass rate delta: 16.67
- Overall score delta: 17.22
- Overall duration delta (s): -19.24
- Agent-token delta: -66920.37
- End-to-end token delta: -56677.21
- Reviewer-token delta: 10243.17
- Claim-done delta: 22.22
- Skill-usage-observed delta: 0.00
- Warm-start tasks compared: 17
- Warm-start pass rate delta: 11.76
- Warm-start score delta: 12.36
- Warm-start duration delta (s): -15.25
- Warm-start agent-token delta: -33844.64
- Warm-start end-to-end token delta: -23486.76
