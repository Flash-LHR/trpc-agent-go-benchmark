//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package gaiaeval

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/metric"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/metric/criterion"
	criterionllm "trpc.group/trpc-go/trpc-agent-go/evaluation/metric/criterion/llm"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/runner/bestofn"
)

const (
	defaultLLMVerifierAttempts       = 3
	defaultLLMVerifierJudgeModelName = "gpt-5.4"
	defaultLLMVerifierJudgeSamples   = 1
	defaultLLMVerifierJudgeMaxTokens = 32768
)

// LLMVerifierConfig contains settings for the GAIA best-of-N verifier runner.
type LLMVerifierConfig struct {
	Attempts       int
	JudgeModelName string
	JudgeSamples   int
	JudgeMaxTokens int
}

// DefaultLLMVerifierConfig returns the default best-of-N verifier settings.
func DefaultLLMVerifierConfig() LLMVerifierConfig {
	return LLMVerifierConfig{
		Attempts:       defaultLLMVerifierAttempts,
		JudgeModelName: defaultLLMVerifierJudgeModelName,
		JudgeSamples:   defaultLLMVerifierJudgeSamples,
		JudgeMaxTokens: defaultLLMVerifierJudgeMaxTokens,
	}
}

func (cfg LLMVerifierConfig) withDefaults() LLMVerifierConfig {
	defaults := DefaultLLMVerifierConfig()
	if cfg.Attempts == 0 {
		cfg.Attempts = defaults.Attempts
	}
	if cfg.JudgeModelName == "" {
		cfg.JudgeModelName = defaults.JudgeModelName
	}
	if cfg.JudgeSamples == 0 {
		cfg.JudgeSamples = defaults.JudgeSamples
	}
	if cfg.JudgeMaxTokens == 0 {
		cfg.JudgeMaxTokens = defaults.JudgeMaxTokens
	}
	return cfg
}

// LLMVerifierRunnerFactory returns a best-of-N runner factory for GAIA.
func LLMVerifierRunnerFactory(_ Config, verifierCfg LLMVerifierConfig) RunnerFactory {
	verifierCfg = verifierCfg.withDefaults()
	return func(ag agent.Agent) (runner.Runner, error) {
		judgeRunner := runner.NewRunner(
			"gaia-llm-verifier-judge",
			newGAIAJudgeAgent(verifierCfg.JudgeModelName, verifierCfg.JudgeMaxTokens),
		)
		bestOfNOpt, err := bestofn.NewRunnerOption(
			bestofn.WithAttempts(verifierCfg.Attempts),
			bestofn.WithAttemptParallelEnabled(true),
			bestofn.WithAttemptParallelism(verifierCfg.Attempts),
			bestofn.WithSelectionMode(bestofn.SelectionModePairwise),
			bestofn.WithEvalMetrics(gaiaLLMVerifierMetric()),
			bestofn.WithRegistry(newGAIAVerifierRegistry()),
			bestofn.WithJudgeRunner(judgeRunner),
			bestofn.WithJudgeRunnerNumSamples(verifierCfg.JudgeSamples),
		)
		if err != nil {
			_ = judgeRunner.Close()
			return nil, err
		}
		return &runnerWithJudge{
			Runner: runner.NewRunner("gaia-runner", ag, bestOfNOpt),
			judge:  judgeRunner,
		}, nil
	}
}

type runnerWithJudge struct {
	runner.Runner
	judge runner.Runner
}

func (r *runnerWithJudge) Close() error {
	err := r.Runner.Close()
	if judgeErr := r.judge.Close(); err == nil {
		err = judgeErr
	}
	return err
}

func newGAIAJudgeAgent(modelName string, maxTokens int) agent.Agent {
	logprobs := true
	topLogprobs := 5
	return llmagent.New("gaia-llm-verifier-judge-agent",
		llmagent.WithModel(openai.New(modelName)),
		llmagent.WithGenerationConfig(model.GenerationConfig{
			MaxTokens:   intPtr(maxTokens),
			Logprobs:    &logprobs,
			TopLogprobs: &topLogprobs,
			Stream:      false,
		}),
	)
}

func gaiaLLMVerifierMetric() *metric.EvalMetric {
	return &metric.EvalMetric{
		MetricName: "llm_verifier_pairwise",
		Threshold:  0.5,
		Criterion: &criterion.Criterion{
			LLMJudge: &criterionllm.LLMCriterion{
				Rubrics: []*criterionllm.Rubric{
					{
						ID: "answer_form_alignment",
						Content: &criterionllm.RubricContent{
							Text: "Prefer the candidate whose final answer matches the form requested by the user, including the requested unit, scale, rounding, ordering, separator style, item count, and whether the answer should be a bare value or include labels.",
						},
					},
					{
						ID: "evidence_grounding",
						Content: &criterionllm.RubricContent{
							Text: "Prefer the candidate whose final answer is directly supported by the visible tool outputs, retrieved source text, calculations, or file contents, especially when the user asks about a specific source, document, image, audio file, rule, or official record.",
						},
					},
					{
						ID: "final_answer_fidelity",
						Content: &criterionllm.RubricContent{
							Text: "Prefer the candidate whose final answer preserves the exact value supported by its own evidence trace. When a trace clearly establishes a value, prefer the candidate that carries that value into the final answer without substituting a nearby, more common, or more fluent-looking answer.",
						},
					},
					{
						ID: "constraint_completion",
						Content: &criterionllm.RubricContent{
							Text: "Prefer the candidate that satisfies all explicit constraints in the request, including source restrictions, date ranges, alphabetic or positional selection rules, formatting constraints, and requested number of items.",
						},
					},
					{
						ID: "concise_extractable_answer",
						Content: &criterionllm.RubricContent{
							Text: "Prefer a concise final answer that is easy to extract when it preserves the requested answer form and the evidence-supported value.",
						},
					},
				},
			},
		},
	}
}

var _ runner.Runner = (*runnerWithJudge)(nil)

func (r *runnerWithJudge) Run(
	ctx context.Context,
	userID string,
	sessionID string,
	message model.Message,
	runOpts ...agent.RunOption,
) (<-chan *event.Event, error) {
	return r.Runner.Run(ctx, userID, sessionID, message, runOpts...)
}
