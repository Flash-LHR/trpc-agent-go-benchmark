package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/planner/react"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

const tauAgentName = "tau-benchmark-agent"

type agentRuntime struct {
	run       runner.Runner
	tools     []tool.Tool
	sessionID string
	timeout   time.Duration
}

func newAgentRuntime(cfg agentConfig) (*agentRuntime, error) {
	tools, err := buildTools(cfg.Tools)
	if err != nil {
		return nil, err
	}
	mdl, err := buildModel(cfg)
	if err != nil {
		return nil, err
	}
	opts, err := llmAgentOptions(cfg, mdl)
	if err != nil {
		return nil, err
	}
	ag := llmagent.New(tauAgentName, opts...)
	run := runner.NewRunner(tauAgentName, ag)
	return &agentRuntime{
		run:       run,
		tools:     tools,
		sessionID: uuid.NewString(),
		timeout:   time.Duration(cfg.TimeoutSeconds) * time.Second,
	}, nil
}

func llmAgentOptions(cfg agentConfig, mdl model.Model) ([]llmagent.Option, error) {
	opts := []llmagent.Option{
		llmagent.WithModel(mdl),
		llmagent.WithInstruction(cfg.SystemPrompt),
		llmagent.WithGenerationConfig(buildGenerationConfig(cfg)),
	}
	switch cfg.Planner {
	case "none":
	case "react":
		opts = append(opts, llmagent.WithPlanner(react.New()))
	default:
		return nil, fmt.Errorf("unsupported trpc planner %q", cfg.Planner)
	}
	return opts, nil
}

func (rt *agentRuntime) generate(ctx context.Context, messages []tauMessage) (*generateResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, rt.timeout)
	defer cancel()
	currentTurn, err := messagesFromTau(messages)
	if err != nil {
		return nil, err
	}
	runOpts := rt.runOptions(currentTurn)
	latestMessage := currentTurn[len(currentTurn)-1]
	events, err := rt.run.Run(ctx, "tau-user", rt.sessionID, latestMessage, runOpts...)
	if err != nil {
		return nil, fmt.Errorf("run llm agent: %w", err)
	}
	return collectResponse(events)
}

func (rt *agentRuntime) runOptions(currentTurn []model.Message) []agent.RunOption {
	opts := []agent.RunOption{agent.WithExternalTools(rt.tools)}
	if len(currentTurn) == 1 {
		return opts
	}
	rewritten := append([]model.Message(nil), currentTurn...)
	opts = append(opts, agent.WithUserMessageRewriter(func(context.Context, *agent.UserMessageRewriteArgs) ([]model.Message, error) {
		return rewritten, nil
	}))
	return opts
}

func (rt *agentRuntime) close() error {
	return rt.run.Close()
}

func buildGenerationConfig(req agentConfig) model.GenerationConfig {
	cfg := model.GenerationConfig{Stream: false}
	if req.MaxTokens > 0 {
		cfg.MaxTokens = intPtr(req.MaxTokens)
	}
	if req.Temperature != nil {
		cfg.Temperature = req.Temperature
	}
	return cfg
}

func intPtr(v int) *int {
	return &v
}
