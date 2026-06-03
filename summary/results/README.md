# Evaluation Results

This directory stores benchmark results for session summarization and on-demand retrieval of hidden context.

## Reports

| File | Description |
|------|-------------|
| [REPORT.md](REPORT.md) | Full evaluation report (English) |
| [REPORT.zh_CN.md](REPORT.zh_CN.md) | Full evaluation report (Chinese) |

## Benchmark Summary

The current report combines three complementary evaluations:

- **MT-Bench-101**: used to study when session summarization is broadly beneficial or harmful
- **QMSum**: used to study whether `summary_ondemand` can recover details hidden by summary compression on meeting transcripts (~19K tokens)
- **LongMemEval**: used to study compact summary and on-demand retrieval on realistic multi-session user/assistant dialogues at extreme context lengths (~103K tokens), including a structured result summary prompt comparison

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
| Highly Recommended | SI, PI, CM | 4.0+ | 28%~40% |
| Conditional | CC, IC, GR | 2.4~3.1 | 4%~10% |
| Not Recommended | SA, SC, TS | 2.0~3.0 | -0.5%~1% |

**Key Insights**:
1. Summarization works well for long dialogues (≥4 turns) with long prompts (>2000 tokens).
2. Summarization harms short dialogues (≤2 turns) due to overhead > compression gains.
3. Current `-events 2` setting is too aggressive for short dialogues.

## QMSum On-Demand Summary

Evaluates `summary_ondemand` on meeting transcripts where supporting evidence is hidden by summary compression.

**Configuration**:
- Dataset: `QMSum` — `test / ALL / specific / support_distance_from_end >= 80`
- Evaluated Cases: `189`
- Model: `gpt-4o-mini`
- Avg Long Context Tokens: ~19K

**Key Results**:

| Metric | Long Context | Summary | Summary + On-Demand |
|--------|-------------:|--------:|--------------------:|
| ROUGE-L | 0.1930 | 0.1516 | 0.1770 |
| F1 | 0.3132 | 0.2238 | 0.2774 |
| Avg Prompt Tokens | 18,986 | 888 | 3,857 |

**Key Insights**:
1. On-demand retrieval recovers ROUGE-L by +0.0255 over plain summary (123 wins, 62 losses).
2. Prompt savings remain large at 76.69% versus long context.
3. The dominant tool path is `session_search → session_load` (1.94 calls/case average).

## LongMemEval On-Demand Retrieval Summary

Evaluates summary behavior on realistic multi-session user/assistant dialogues at extreme context lengths. The current reported LongMemEval run uses compact summaries with on-demand retrieval enabled to test whether focused retrieval can recover facts hidden by summary compression. It also compares a structured result summary prompt: a nine-section summary format that asks the model to preserve user messages verbatim.

**Configuration**:
- Dataset: `LongMemEval` — `single-session-user`
- Evaluated Cases: `70`
- Model: `gpt-4o-mini`
- Avg Long Context Tokens: ~103K
- Summary Trigger: `-events 40`, visible tail: `-lme-visible-events 20`
- Prompt Variants: compact summary; structured result summary

**Key Results**:

| Configuration | Mode | ROUGE-L | LLMScore | Exact Match | Avg Prompt Tokens | Prompt Savings |
|---------------|------|--------:|---------:|------------:|------------------:|---------------:|
| Full context | `long_context` | 0.1192 | 0.7386 | 0.6571 | 103,565 | — |
| Compact summary | `summary` | 0.0477 | 0.0907 | 0.0143 | 445 | 99.57% |
| Compact summary + retrieval | `summary_ondemand` | **0.2694** | **0.9000** | **0.7571** | 6,182 | 94.04% |

**Prompt Variant Check**:

| Summary Prompt | Mode | ROUGE-L | LLMScore | Exact Match | Avg Summary Chars |
|----------------|------|--------:|---------:|------------:|------------------:|
| Compact summary | `summary_ondemand` | **0.2694** | **0.9000** | 0.7571 | 1,745 |
| Structured result summary | `summary_ondemand` | 0.2528 | 0.8879 | 0.7571 | 2,150 |

**Key Insights**:
1. Compact summary is extremely token-efficient but too lossy for LongMemEval summary-only recall.
2. Compact summary + on-demand retrieval is a strong low-token option: ROUGE-L 0.2694 with 94.04% prompt savings.
3. On-demand retrieval exceeds full context on this slice: exact match improves from 0.6571 to 0.7571 while using far fewer prompt tokens.
4. Structured result summary is longer but does not improve LongMemEval headline metrics, so compact summary remains the better default for this workload.
5. A small number of overlong events failed embedding due to the 8192-token per-input limit, so the retrieval result is slightly conservative.
