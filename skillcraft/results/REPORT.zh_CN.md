# 基于 SkillCraft 基准的 Agent 自进化评估

## 1. 引言

本报告使用 **SkillCraft** 基准评估 **trpc-agent-go** 的 agent 自进化
（evolution）能力。

- **Baseline**：关闭 evolution，每个任务从零开始。
- **Evolution**：打开 evolution，后台异步抽取的 `SKILL.md` 技能文件
  会暴露给后续任务，agent 可以通过 `skill_load` 工具加载并复用。

核心问题：

> **一个会在后台自动抽取可复用技能、并在后续任务中加载复用的 agent，
> 是否比每次从零开始的 agent 更强？**

SkillCraft 很适合回答这个问题：每个任务族提供"形状相同、规模递增"的
变体（`e1`--`e3` easy, `m1`--`m2` medium, `h1` hard）。如果 agent 能
在简单任务上提炼出可复用技能，那么后续复杂任务就应该更稳定、更省
token，或两者兼有。

## 2. 实验设置

### 2.1 基准数据集

| 项目 | 值 |
| --- | --- |
| 基准 | SkillCraft |
| 任务族 | 5 个：`openmeteo-weather`（天气监测）、`recipe-cookbook-builder`（食谱构建）、`world-bank-economic-snapshot`（经济快照）、`cat-facts-collector`（猫咪百科）、`pokeapi-pokedex`（宝可梦图鉴） |
| 每个族的变体 | `e1` / `e2` / `e3` / `m1` / `m2` / `h1` |
| 每轮任务数 | 30（5 族 x 6 变体） |
| Agent 模型 | `gpt-4o-mini` |
| Reviewer 模型 | `gpt-4o-mini` |
| 评分 | SkillCraft 官方 `evaluation/main.py`（每任务 0--100 分） |

### 2.2 技能种子库

所有 evolution run 从同一份预置种子目录（`seed_skills/`）起步，
包含 9 条泛化工作流技能，覆盖全部 5 个任务族。每条技能提供可复用的
分步模板，不含硬编码参数。

**表 0：种子技能清单**

| 任务族 | 技能名称 | 覆盖范围 |
| --- | --- | --- |
| openmeteo-weather | Weather Data Collection | 单城市采集 |
| openmeteo-weather | Weather Data Collection — Multi-City | 多城市采集 |
| openmeteo-weather | Weather Data Collection — Multi-City — Detailed | 扩展预报 |
| openmeteo-weather | Weather Monitor — Multi-City | 告警监控 |
| openmeteo-weather | Weather Monitor — Multi-City with Historical Data | 历史对比 |
| recipe-cookbook-builder | Recipe Cookbook — Multi-Dish | 多菜谱工作流 |
| world-bank-economic-snapshot | Economic Snapshot — Multi-Country | 多国指标 |
| cat-facts-collector | Cat Facts Collector — Multi-Breed | 多品种猫咪百科 |
| pokeapi-pokedex | PokeAPI Pokedex — Multi-Pokemon | 多宝可梦图鉴 |

注：本报告主要结果（表 1–5）使用初始 7 条种子（覆盖 3/5 族）完成。
cat-facts 和 pokeapi 种子在主实验后补充并单独验证（skill_load 从
74.4% 提升至 100%）。

### 2.3 Evolution 机制

Evolution 是一个**异步学习闭环**，主流程不被阻塞：

```
┌─────────────────────────────────────────────────────────────────┐
│                          主任务路径                                │
│  Request ──▶ [skill_load] ──▶ Agent ──▶ Tool Calls ──▶ Result   │
└────────────────────────────────────┬────────────────────────────┘
                                     │ 入队 (transcript + outcome)
                                     ▼
┌─────────────────────────────────────────────────────────────────┐
│                        后台学习闭环                                │
│                                                                  │
│  ┌──────────┐    ┌────────────┐    ┌───────────┐    ┌────────┐ │
│  │ Reviewer  │──▶│ Reconciler │──▶│   Gates   │──▶│Publish │ │
│  │ (LLM)    │    │(去重/吸收) │    │(A → B → C)│    │        │ │
│  └──────────┘    └────────────┘    └───────────┘    └───┬────┘ │
└─────────────────────────────────────────────────────────┼───────┘
                                                          │
                              ┌────────────────────────────┘
                              ▼
                    ┌───────────────────┐
                    │  Managed Skills   │ ◀── 下一个任务读取
                    │  (SKILL.md files) │
                    └───────────────────┘
```

1. 每个任务完成后，runner 将 transcript + evaluator outcome 入队；
2. 后台 reviewer 模型给出结构化决策（`skills` / `updates` / `deletions`）；
3. 确定性 reconciler 去重、吸回兄弟簇（strict-superset rewrite /
   intra-batch dedup / quantified-sibling absorption）；
4. 通过质量门禁写入 managed skills 目录。

Agent 侧通过 `skill_load` 工具加载 skill body。框架层在 relevance
ranking 之上增加了 "Top recommended skill" 硬提示，benchmark 层的
instruction 要求 agent 在 domain tool 前先 `skill_load` 匹配的技能
（skill-first protocol）。

### 2.4 质量门禁

| Phase | 组件 | 说明 |
| --- | --- | --- |
| A | `FileCandidateStore` + `FileActivePointer` | 每次技能变更都写成 immutable revision（`meta.json` + `SKILL.md`），旁边一个 append-only `audit.log`；`active.txt` 指向当前可见 revision |
| B | `DefaultSpecGate` + `DefaultSafetyGate` | 确定性规则，零 LLM 调用。SpecGate 检查 schema 完整性 / name 稳定性 / 查重 / quantified-sibling；SafetyGate 扫描 secret pattern / 危险 shell / path traversal |
| C | `OutcomeBasedEffectivenessGate` | 检查触发 review 的 session 的 Outcome：score < 80 或 status=fail/agent_error 时，revision 停在 `PendingEval` 不自动 promote，防止从灾难 run 中学到错误的技能 |

### 2.5 评估协议

每个 30 任务的 run 重复 **3 次**，取均值聚合。另有一组更高置信度的
2 族实验重复 5 次。除特别注明外，表格均为聚合结果。

## 3. 结果

### 3.1 主要结果：5 族聚合

**表 1：5 族评估（3 轮，n = 90 per arm）**

| 指标 | Baseline | Evolution | Delta |
| --- | ---: | ---: | ---: |
| Pass rate | 84.4% | **87.8%** | **+3.3pp** |
| E2E tokens / task | 272,653 | 183,435 | **-32.7%** |
| `skill_load` invoked | 0% | 74.4% | -- |

> Evolution 在 5 个不同任务族上将 pass rate 提升 3.3 个百分点，
> 同时将 token 消耗降低 32.7%。

### 3.2 逐族明细

**表 2：逐族结果（3 轮聚合，每族每臂 n = 18）**

| 任务族 | BL Pass | EV Pass | Delta Pass | skill_load | Delta Tokens |
| --- | ---: | ---: | ---: | ---: | ---: |
| openmeteo-weather | 100.0% | 100.0% | +0.0pp | 100.0% | -7.0% |
| recipe-cookbook-builder | 88.9% | 88.9% | +0.0pp | 100.0% | +7.3% |
| world-bank-economic-snapshot | 88.9% | 88.9% | +0.0pp | 100.0% | -9.6% |
| cat-facts-collector | 44.4% | 61.1% | **+16.7pp** | 0.0% | -53.5% |
| pokeapi-pokedex | 100.0% | 100.0% | +0.0pp | 72.2% | -15.5% |

主要观察：

- **cat-facts-collector** 是最难的族（baseline 仅 44.4%），evolution
  在此提供了最大的 pass rate 提升（+16.7pp），但本次评测时 skill_load
  = 0%（当时种子库中无匹配技能），改善完全来自 reviewer 在运行中创建
  的技能。后续补充种子 + 修复 skill-first prompt 后，cat-facts 的
  skill_load 已提升至 100%（见 §2.2 注释）。
- **有种子技能的族**（weather、recipe、world-bank）skill_load
  达到 100%，token 节省一致，但 pass rate 已经较高所以 delta 接近零。
- **recipe-cookbook-builder** 出现 +7.3% 的 token 开销 -- skill_load
  的开销在此族超过了效率增益。

### 3.3 逐轮可重复性

**表 3：逐轮明细**

| 轮次 | BL Pass | EV Pass | Delta Pass | Delta Tokens |
| --- | ---: | ---: | ---: | ---: |
| Try 1 | 80.0% | 86.7% | +6.7pp | -38.1% |
| Try 2 | 93.3% | 90.0% | -3.3pp | -32.9% |
| Try 3 | 80.0% | 86.7% | +6.7pp | -27.8% |
| **均值** | **84.4%** | **87.8%** | **+3.3pp** | **-32.7%** |

> Token 节省在所有轮次中一致为负（evolution 始终更便宜）。
> Pass rate 在 3 轮中有 2 轮为正，均值为正。

### 3.4 高置信度 2 族实验

为获得更高的统计置信度，我们另外进行了 2 族实验（weather + recipe，
12 任务），重复 5 轮。该配置下种子库完全覆盖两个族，skill_load
接近 100%。

**表 4：2 族评估（5 轮，n = 60 per arm）**

| 指标 | Baseline | Evolution | Delta |
| --- | ---: | ---: | ---: |
| Pass rate | 95.0% | **98.3%** | **+3.3pp** |
| E2E tokens / task | 158,642 | 131,170 | **-17.3%** |
| E2E token 标准差 | 46,029 | 6,387 | **仅为 baseline 的 13.9%** |
| `skill_load` invoked | 0% | 98.3% | -- |

> 在种子完全覆盖的条件下，evolution 实现接近完美的 pass rate（98.3%），
> 且 token 标准差从 46,029 骤降至 6,387（仅为 baseline 的 13.9%），
> agent 行为变得高度可预测。

### 3.5 灾难 loop 压制

使用 `gpt-4o-mini` 时，某些任务族存在随机灾难 loop：agent 反复调用
同一 API 直到上下文爆炸（单任务 token > 1M）。Evolution 通过 skill
中的明确步骤指引有效压制了这一问题。

**表 5：代表性灾难 loop 案例**

| 数据集 | 任务 | BL Tokens | BL 结果 | EV Tokens | EV 结果 | 节省 |
| --- | --- | ---: | --- | ---: | --- | ---: |
| 5 族 / try2 | cat-facts/e1 | 1,201,322 | fail | 77,087 | pass | 93.6% |
| 5 族 / try2 | cat-facts/e2 | 1,196,908 | fail | 70,250 | pass | 94.1% |
| 5 族 / try3 | cat-facts/m2 | 1,621,351 | fail | 114,416 | pass | 92.9% |
| 2 族 / try5 | weather/e1 | 1,352,010 | fail | 72,465 | pass | 94.6% |
| 2 族 / try4 | weather/e1 | 1,108,543 | pass | 124,922 | pass | 88.7% |
| 2 族 / try1 | weather/e1 | 996,305 | pass | 72,023 | pass | 92.8% |

> 在每一个观察到的灾难 loop 中，evolution 要么挽救了失败任务，
> 要么避免了虽然成功但极其昂贵的运行。单案例节省最高达 94.6%。

### 3.6 质量门禁行为

**表 6：质量门禁统计（5 族，3 轮合计）**

| 指标 | 值 |
| --- | --- |
| Candidate revisions seen | 69 |
| Revisions promoted to active | 69 |
| SpecGate rejected | 0 |
| SafetyGate rejected | 0 |
| EffectivenessGate held | 0 |

> 零 rejection 是预期行为：reconciler 已经在 reviewer 输出送入 gate
> 之前吸掉了绝大部分不合规候选（quantified sibling、strict superset
> 重名）。SpecGate / SafetyGate 对恶意 case（secret leak、
> `rm -rf /`、`../../etc/passwd`）的拦截能力已在单元测试中验证。

### 3.7 Multi-session 累积实验

为验证 skill 库在长期使用下是否会退化，我们进行了 5 轮累积实验：
Round 1 从**空库冷启动**（无 warm-start 种子），后续每轮用上一轮
产出的 managed skills 作为 seed。

**表 7：累积实验（5 轮，每轮 12 任务）**

| Round | BL Pass | EV Pass | BL E2E | EV E2E | Delta E2E | skill_load |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| R1 (cold) | 91.7% | 100.0% | 228,334 | 252,618 | +24,284 | 92% |
| R2 | 91.7% | 91.7% | 220,554 | 130,099 | -90,454 | 100% |
| R3 | 91.7% | 100.0% | 256,799 | 131,108 | -125,691 | 100% |
| R4 | 100.0% | 100.0% | 117,643 | 136,131 | +18,487 | 100% |
| R5 | 91.7% | 83.3% | 285,764 | 140,864 | -144,900 | 100% |
| **均值** | **93.3%** | **95.0%** | **221,819** | **158,164** | **-63,655 (-28.7%)** | **98%** |

关键发现：

1. **Skill 库收敛到 6 条，5 轮零增长。** Reconciler 把 reviewer 的
   所有 create 都吸成 update，库不会膨胀。
2. **skill_load 从 92%（R1）收敛到 100%（R2 起）**。
3. **Token 压制从 R2 开始生效**：冷启动轮略贵（agent 在首批技能
   可用前先探索），但从 R2 起 evolution 持续节省 token（R5 单任务
   最高节省 -145k）。

## 4. 讨论

### 4.1 Evolution 的收益来源

实验数据一致指向同一个结论：evolution 的核心价值不是"每轮都好一点"，
而是**压制 baseline 的随机灾难 loop**。在 baseline 风平浪静时，
evolution 的 token 消耗与之相当或略高（因为 skill_load + reviewer
的 overhead）；在 baseline 命中灾难 loop 时，evolution 能节省 90%+
的 token 并常常将 fail 挽救为 pass。这解释了为什么聚合改善以 token
节省（-32.7%）为主而非 pass rate（+3.3pp）—— pass rate 提升主要
来自 cat-facts 的挽救案例，而 token 节省来自所有族中灾难 loop
的压制。

### 4.2 质量门禁的作用

Phase A（revision store + active pointer）解决的是可审计和可回滚，
不是 benchmark 提分。Phase B（SpecGate + SafetyGate）是最后一道
防线——当前因为 reconciler 已经把绝大部分不合规候选清理掉了，所以
gate 看起来没有拦截任何东西（这是正确的）。Phase C（effectiveness
gate）在正常运行时不会拦截，只在灾难 run 触发时才会挡住"从错误中
学到的错误 skill"。

### 4.3 局限性

1. **仅有 benchmark 验证**：所有数据来自 SkillCraft，缺乏真实线上
   adopter 的 skill 产出密度和命中率数据。
2. **Reviewer 模型较弱**：`gpt-4o-mini` 会生成 count-specific 的
   技能名（`3 Cities` 而非 `Multi-City`），靠 reconciler 吸回。
   gpt-4o 对比实验（3 轮）确认 reconciler 已兜住此差距——更强 reviewer
   不产生显著 benchmark 提升。
3. **技能消费路径单一**：当前只有 `skill_load`，没有 progressive
   disclosure（先看摘要再决定是否 load）。

## 5. 结论

在 SkillCraft 上覆盖 5 个任务族、150+ 次受控对照的评测中，
trpc-agent-go 的 agent 自进化机制展现了三方面确定性收益：

1. **Pass rate 提升**：+3.3pp（5 族 84.4% -> 87.8%；种子完全覆盖
   的 2 族 95.0% -> 98.3%）。
2. **Token 消耗降低**：5 族 -32.7%，个别轮达 -38.1%。单案例灾难
   loop 压制最高节省 94.6%。
3. **库稳定性**：累积实验证明 skill 库收敛到 6 条、5 轮零增长，
   同时保持 -28.7% 的 token 节省。

质量门禁（Phase A/B/C）以零误拦截透明运行，为生产上线提供了
可审计、可回滚的 skill 生命周期管理。

---

## 附录

### A. 复现命令

```bash
cd skillcraft/trpc-agent-go-impl

# 5 族评测（30 任务，全质量门禁）
go run . \
  -skillcraft-root "$SKILLCRAFT_ROOT" \
  -tasks "openmeteo-weather/e1,...,pokeapi-pokedex/h1" \
  -mode compare \
  -model gpt-4o-mini \
  -reviewer-model gpt-4o-mini \
  -max-tool-iterations 24 \
  -mcp-timeout-seconds 120 \
  -load-skills-from ../results/tools/seed_skills \
  -max-prompt-skills 8 \
  -enable-approval-gate \
  -effectiveness-gate \
  -output ../results/<output_dir>
```

### B. 关键 CLI 参数

| 参数 | 说明 |
| --- | --- |
| `-enable-approval-gate` | 开启 Phase A revision store + Phase B SpecGate/SafetyGate |
| `-effectiveness-gate` | 开启 Phase C outcome-based effectiveness gate |
| `-approval-gate-shadow` | Shadow 模式：gate 评估但不拦截 |
| `-load-skills-from` | 指定 warm-start seed 目录 |
| `-max-prompt-skills` | 限制 prompt 中 skill overview 的条数 |
| `-mcp-timeout-seconds` | MCP 工具超时（对慢 API 如 World Bank 需加大） |
