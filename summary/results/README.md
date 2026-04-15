# Evaluation Results

This directory stores benchmark results for session summarization and on-demand retrieval of hidden context.

## Reports

| File | Description |
|------|-------------|
| [REPORT.md](REPORT.md) | Full evaluation report (English) |
| [REPORT.zh_CN.md](REPORT.zh_CN.md) | Full evaluation report (Chinese) |

## Benchmark Summary

The current report combines two complementary evaluations:

- **MT-Bench-101**: used to study when session summarization is broadly beneficial or harmful
- **QMSum**: used to study whether `summary_ondemand` can recover details hidden by summary compression through on-demand retrieval

## MT-Bench-101 Evaluation Summary

**Configuration**:
- Model: deepseek-v3.2
- Summary Trigger: Every 2 turns (`-events 2`)
- Total Cases: 917 (9 tasks)

**Key Results**:

| Metric | Value |
|--------|------:|
| Overall Prompt Savings | 24.47% |
| Overall Token Savings | 12.89% |
| Weighted Consistency | 0.853 |
| Pass^1 Rate | 92.3% |
| Negative Token Cases | 35.9% |

**Task Suitability**:

| Suitability | Tasks | Avg Turns | Prompt Savings |
|-------------|-------|----------:|---------------:|
| ✅ Highly Recommended | SI, PI, CM | 4.0+ | 28%~40% |
| ⚠️ Conditional | CC, IC, GR | 2.4~3.1 | 4%~10% |
| ❌ Not Recommended | SA, SC, TS | 2.0~3.0 | -0.5%~1% |

**Key Insights**:
1. Summarization works well for long dialogues (≥4 turns) with long prompts (>2000 tokens).
2. Summarization harms short dialogues (≤2 turns) due to overhead > compression gains.
3. Current `-events 2` setting is too aggressive for short dialogues.

## QMSum On-Demand Summary

We also evaluate whether `summary_ondemand` can recover details hidden by summary compression on a broader QMSum hidden-detail workload.

**Configuration**:
- Dataset: `QMSum`
- Slice: `test / ALL / specific / support_distance_from_end >= 80`
- Loaded Cases: `244`
- Evaluated Cases: `189`
- Model: `gpt-4o-mini`
- Summary Trigger: `-events 40`
- Visible Event Window: `-qmsum-visible-events 20`

**Key Results**:

| Metric | Long Context | Summary | Summary + On-Demand Retrieval |
|--------|-------------:|--------:|--------------------:|
| ROUGE-L | 0.1930 | 0.1516 | 0.1770 |
| F1 | 0.3132 | 0.2238 | 0.2774 |
| BLEU | 0.2490 | 0.1651 | 0.2351 |
| Avg Prompt Tokens | 18,986 | 888 | 3,857 |
| Avg Query Latency | 4,556 ms | 2,994 ms | 8,656 ms |

**Key Insights**:
1. Plain summary saves tokens aggressively but creates a real hidden-detail quality gap.
2. On-demand retrieval recovers a meaningful portion of that loss: ROUGE-L improves by `+0.0255` over summary.
3. Recovery remains cost-effective: summary + on-demand retrieval still preserves `76.69%` prompt savings versus long context.
4. On the evaluated slice, per-case ROUGE-L is `123` wins, `62` losses, and `4` ties against plain summary.
