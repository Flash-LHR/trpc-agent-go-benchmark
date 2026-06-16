//
// Tencent is pleased to support the open source community by making
// trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

// Package main runs SkillCraft tasks with trpc-agent-go and compares a plain
// baseline against the evolution skill-learning loop.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/evolution"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	mcpcfg "trpc.group/trpc-go/trpc-agent-go/tool/mcp"
)

type runMode string

const (
	modeBaseline  runMode = "baseline"
	modeEvolution runMode = "evolution"
	modeCompare   runMode = "compare"
)

// modeLearnsSkills reports whether the given mode runs the background
// evolution reviewer + publisher pipeline. trpc-agent-go intentionally
// keeps skill management on the reviewer-driven async path, so this is
// a single-mode predicate today; it stays as a function for clarity at
// the call sites and to make future modes easy to add.
func modeLearnsSkills(m runMode) bool {
	return m == modeEvolution
}

const (
	defaultTaskRoot          = "tasks/scaled_tasks"
	defaultOutputRelative    = "../results"
	defaultAgentMaxTokens    = 4096
	defaultToolIterations    = 16
	defaultFallbackTaskLimit = 900
	defaultMCPTimeoutSec     = 60
)

var (
	flagSkillCraftRoot = flag.String(
		"skillcraft-root",
		envOrDefault("SKILLCRAFT_ROOT", ""),
		"Path to the local SkillCraft checkout",
	)
	flagTaskRoot = flag.String(
		"task-root",
		defaultTaskRoot,
		"Task root relative to SkillCraft root",
	)
	flagBaseTask = flag.String(
		"base-task",
		"",
		"Base task family under task-root, e.g. cat-facts-collector",
	)
	flagScales = flag.String(
		"scales",
		"e1,e2,e3",
		"Comma-separated task scales when -base-task is used",
	)
	flagTasks = flag.String(
		"tasks",
		"",
		"Explicit comma-separated task directories (relative to SkillCraft root or absolute paths)",
	)
	flagMode = flag.String(
		"mode",
		string(modeCompare),
		"Run mode: baseline, evolution, or compare (runs baseline + evolution back-to-back)",
	)
	flagModel = flag.String(
		"model",
		envOrDefault("MODEL_NAME", "gpt-4o-mini"),
		"Agent model name",
	)
	flagReviewerModel = flag.String(
		"reviewer-model",
		"",
		"Evolution reviewer model name (defaults to -model)",
	)
	flagVariant = flag.String(
		"variant",
		"",
		"Optional OpenAI-compatible provider variant",
	)
	flagOutput = flag.String(
		"output",
		defaultOutputRelative,
		"Output directory for benchmark results",
	)
	flagTaskTimeoutSeconds = flag.Int(
		"task-timeout-seconds",
		0,
		"Per-task timeout override in seconds (0 = use SkillCraft task_config timeout)",
	)
	flagMaxToolIterations = flag.Int(
		"max-tool-iterations",
		defaultToolIterations,
		"Maximum tool iterations per task",
	)
	flagMaxTokens = flag.Int(
		"max-tokens",
		defaultAgentMaxTokens,
		"Maximum output tokens per model call",
	)
	flagMCPTimeoutSeconds = flag.Int(
		"mcp-timeout-seconds",
		defaultMCPTimeoutSec,
		"MCP server init/call timeout in seconds",
	)
	flagKeepWorkspaces = flag.Bool(
		"keep-workspaces",
		true,
		"Keep per-task workspaces after evaluation",
	)
	flagVerbose = flag.Bool(
		"verbose",
		false,
		"Print verbose per-task progress",
	)
	flagLoadSkillsFrom = flag.String(
		"load-skills-from",
		"",
		"Optional managed_skills directory to seed evolution mode with before the run. "+
			"Each subdirectory is treated as one skill and copied verbatim into "+
			"<output>/managed_skills/. Useful for accumulating skills across runs and "+
			"for exercising the failure-aware learning loop on already-warm libraries.",
	)
	flagReviewerSkillBodyChars = flag.Int(
		"reviewer-skill-body-chars",
		0,
		"Per-skill body excerpt budget (chars) the reviewer sees for each existing "+
			"skill in the library. 0 uses the framework default; a negative value "+
			"disables bodies and shows description-only entries. Increase when "+
			"existing SKILL.md files are long enough that the head excerpt misses "+
			"meaningful procedural content.",
	)
	flagMaxPromptSkills = flag.Int(
		"max-prompt-skills",
		0,
		"Cap on the number of managed-skill summaries (name + full description) "+
			"injected into the agent's system prompt for evolution-mode tasks. 0 "+
			"means no cap (default). When positive and the library exceeds the cap, "+
			"only the first N summaries are rendered with descriptions; remaining "+
			"skill names are listed as a compact tail so skill_load can still reach "+
			"them. Use this to keep large warm-start libraries from pushing the "+
			"prompt close to the model's context window.",
	)
	flagEnableApprovalGate = flag.Bool(
		"enable-approval-gate",
		false,
		"Route reviewer output through the Phase A approval gate "+
			"(immutable revision store + deterministic SpecGate / SafetyGate). "+
			"Rejected revisions are logged to the candidate store audit log but "+
			"never become active. Off by default so legacy runs still use the "+
			"direct publish path.",
	)
	flagApprovalGateShadow = flag.Bool(
		"approval-gate-shadow",
		false,
		"Run the approval gate in shadow mode: gates are evaluated and "+
			"revisions are written to the candidate store, but the live "+
			"Publisher is still updated even when gates reject. Useful for "+
			"comparing gate verdicts against the pre-gate behavior without "+
			"blocking any reviewer output. Only meaningful when "+
			"-enable-approval-gate is set.",
	)
	flagEffectivenessGate = flag.Bool(
		"effectiveness-gate",
		false,
		"Enable the Phase C outcome-based effectiveness gate. When set "+
			"(together with -enable-approval-gate), revisions extracted from "+
			"sessions that failed or scored below 80%% are held in "+
			"pending_eval status instead of being auto-promoted. This "+
			"prevents learning from catastrophic runs.",
	)
)

type benchmarkConfig struct {
	SkillCraftRoot         string
	TaskRoot               string
	BaseTask               string
	Scales                 []string
	ExplicitTasks          []string
	Mode                   runMode
	ModelName              string
	ReviewerModelName      string
	Variant                string
	OutputDir              string
	TaskTimeout            time.Duration
	MaxToolIterations      int
	MaxTokens              int
	MCPTimeout             time.Duration
	KeepWorkspaces         bool
	Verbose                bool
	LoadSkillsFrom         string
	ReviewerSkillBodyChars int
	MaxPromptSkills        int
	EnableApprovalGate     bool
	ApprovalGateShadow     bool
	EffectivenessGate      bool
}

type rawTaskConfig struct {
	TaskName         string   `json:"task_name"`
	TaskType         string   `json:"task_type"`
	NeededMCPServers []string `json:"needed_mcp_servers"`
	NeededLocalTools []string `json:"needed_local_tools"`
	MaxTurns         int      `json:"max_turns"`
	Timeout          int      `json:"timeout"`
	Meta             struct {
		BaseTask    string `json:"base_task"`
		ScaleLevel  string `json:"scale_level"`
		Difficulty  string `json:"difficulty"`
		Description string `json:"description"`
	} `json:"meta"`
}

type taskDefinition struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	BaseTask          string   `json:"baseTask"`
	Scale             string   `json:"scale"`
	Difficulty        string   `json:"difficulty"`
	Description       string   `json:"description"`
	Dir               string   `json:"dir"`
	TaskDocPath       string   `json:"taskDocPath"`
	TaskDoc           string   `json:"taskDoc"`
	AgentPromptPath   string   `json:"agentPromptPath,omitempty"`
	AgentPrompt       string   `json:"agentPrompt,omitempty"`
	EvaluationScript  string   `json:"evaluationScript"`
	InitialWorkspace  string   `json:"initialWorkspace,omitempty"`
	NeededMCPServers  []string `json:"neededMCPServers"`
	NeededLocalTools  []string `json:"neededLocalTools"`
	MaxTurns          int      `json:"maxTurns"`
	TimeoutSeconds    int      `json:"timeoutSeconds"`
	HasInitialContent bool     `json:"hasInitialContent"`
}

type benchmarkResult struct {
	Timestamp         string         `json:"timestamp"`
	RequestedMode     runMode        `json:"requestedMode"`
	Model             string         `json:"model"`
	ReviewerModel     string         `json:"reviewerModel"`
	Variant           string         `json:"variant,omitempty"`
	SkillCraftRoot    string         `json:"skillcraftRoot"`
	TaskRoot          string         `json:"taskRoot"`
	BaseTask          string         `json:"baseTask,omitempty"`
	Scales            []string       `json:"scales,omitempty"`
	Tasks             []*taskSummary `json:"tasks"`
	Baseline          *modeResult    `json:"baseline,omitempty"`
	Evolution         *modeResult    `json:"evolution,omitempty"`
	Comparison        *compareResult `json:"comparison,omitempty"`
	OutputDir         string         `json:"outputDir"`
	KeepWorkspaces    bool           `json:"keepWorkspaces"`
	MaxToolIterations int            `json:"maxToolIterations"`
	TaskTimeoutSec    int            `json:"taskTimeoutSeconds"`
}

type taskSummary struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	BaseTask          string   `json:"baseTask"`
	Scale             string   `json:"scale"`
	Difficulty        string   `json:"difficulty"`
	Description       string   `json:"description"`
	NeededMCPServers  []string `json:"neededMCPServers"`
	NeededLocalTools  []string `json:"neededLocalTools"`
	MaxTurns          int      `json:"maxTurns"`
	TimeoutSeconds    int      `json:"timeoutSeconds"`
	HasInitialContent bool     `json:"hasInitialContent"`
}

type modeResult struct {
	Mode    runMode          `json:"mode"`
	Cases   []*taskRunResult `json:"cases"`
	Summary *modeSummary     `json:"summary"`
}

type subsetSummary struct {
	Tasks                 int     `json:"tasks"`
	PassedTasks           int     `json:"passedTasks"`
	PassRate              float64 `json:"passRate"`
	AverageScorePercent   float64 `json:"averageScorePercent"`
	AverageDurationSec    float64 `json:"averageDurationSeconds"`
	AveragePromptTokens   float64 `json:"averagePromptTokens"`
	AverageOutputTokens   float64 `json:"averageOutputTokens"`
	AverageTotalTokens    float64 `json:"averageTotalTokens"`
	AverageReviewerTokens float64 `json:"averageReviewerTokens,omitempty"`
	AverageEndToEndTokens float64 `json:"averageEndToEndTokens,omitempty"`
	ClaimDoneRate         float64 `json:"claimDoneRate"`
	SkillsOfferedTasks    int     `json:"skillsOfferedTasks,omitempty"`
	SkillsOfferedRate     float64 `json:"skillsOfferedRate,omitempty"`
	SkillToolInvokedTasks int     `json:"skillToolInvokedTasks,omitempty"`
	SkillToolInvokedRate  float64 `json:"skillToolInvokedRate,omitempty"`
}

type modeSummary struct {
	Tasks                    int            `json:"tasks"`
	PassedTasks              int            `json:"passedTasks"`
	PassRate                 float64        `json:"passRate"`
	AverageScorePercent      float64        `json:"averageScorePercent"`
	AverageDurationSec       float64        `json:"averageDurationSeconds"`
	AveragePromptTokens      float64        `json:"averagePromptTokens"`
	AverageOutputTokens      float64        `json:"averageOutputTokens"`
	AverageTotalTokens       float64        `json:"averageTotalTokens"`
	AverageReviewerTokens    float64        `json:"averageReviewerTokens,omitempty"`
	AverageEndToEndTokens    float64        `json:"averageEndToEndTokens,omitempty"`
	ClaimDoneRate            float64        `json:"claimDoneRate"`
	SkillsOfferedTasks       int            `json:"skillsOfferedTasks,omitempty"`
	SkillsOfferedRate        float64        `json:"skillsOfferedRate,omitempty"`
	SkillToolInvokedTasks    int            `json:"skillToolInvokedTasks,omitempty"`
	SkillToolInvokedRate     float64        `json:"skillToolInvokedRate,omitempty"`
	ReviewerPromptTokens     int            `json:"reviewerPromptTokens,omitempty"`
	ReviewerCompletionTokens int            `json:"reviewerCompletionTokens,omitempty"`
	ReviewerTotalTokens      int            `json:"reviewerTotalTokens,omitempty"`
	AgentErrorCount          int            `json:"agentErrorCount"`
	EvalErrorCount           int            `json:"evalErrorCount"`
	SkillsGenerated          int            `json:"skillsGenerated,omitempty"`
	FinalSkillNames          []string       `json:"finalSkillNames,omitempty"`
	WarmStart                *subsetSummary `json:"warmStart,omitempty"`
	ColdStart                *subsetSummary `json:"coldStart,omitempty"`
}

type compareResult struct {
	PassRateDelta               float64 `json:"passRateDelta"`
	ScoreDelta                  float64 `json:"scoreDelta"`
	DurationDeltaSec            float64 `json:"durationDeltaSeconds"`
	TokenDelta                  float64 `json:"tokenDelta"`
	EndToEndTokenDelta          float64 `json:"endToEndTokenDelta"`
	ReviewerTokenDelta          float64 `json:"reviewerTokenDelta"`
	ClaimDoneDelta              float64 `json:"claimDoneDelta"`
	SkillsOfferedDelta          float64 `json:"skillsOfferedDelta"`
	SkillToolInvokedDelta       float64 `json:"skillToolInvokedDelta"`
	WarmStartTaskCount          int     `json:"warmStartTaskCount,omitempty"`
	WarmStartPassRateDelta      float64 `json:"warmStartPassRateDelta,omitempty"`
	WarmStartScoreDelta         float64 `json:"warmStartScoreDelta,omitempty"`
	WarmStartDurationDeltaSec   float64 `json:"warmStartDurationDeltaSeconds,omitempty"`
	WarmStartTokenDelta         float64 `json:"warmStartTokenDelta,omitempty"`
	WarmStartEndToEndTokenDelta float64 `json:"warmStartEndToEndTokenDelta,omitempty"`
}

type taskRunResult struct {
	TaskID                   string                         `json:"taskId"`
	TaskName                 string                         `json:"taskName"`
	BaseTask                 string                         `json:"baseTask"`
	Scale                    string                         `json:"scale"`
	Mode                     runMode                        `json:"mode"`
	Status                   string                         `json:"status"`
	DurationSeconds          float64                        `json:"durationSeconds"`
	PromptTokens             int                            `json:"promptTokens"`
	CompletionTokens         int                            `json:"completionTokens"`
	TotalTokens              int                            `json:"totalTokens"`
	ReviewerPromptTokens     int                            `json:"reviewerPromptTokens,omitempty"`
	ReviewerCompletionTokens int                            `json:"reviewerCompletionTokens,omitempty"`
	ReviewerTotalTokens      int                            `json:"reviewerTotalTokens,omitempty"`
	EndToEndTotalTokens      int                            `json:"endToEndTotalTokens,omitempty"`
	ToolCalls                []string                       `json:"toolCalls,omitempty"`
	SkillToolCalls           []string                       `json:"skillToolCalls,omitempty"`
	LoadedSkillNames         []string                       `json:"loadedSkillNames,omitempty"`
	ClaimDoneCalled          bool                           `json:"claimDoneCalled"`
	HadAvailableSkills       bool                           `json:"hadAvailableSkills,omitempty"`
	SkillToolInvoked         bool                           `json:"skillToolInvoked,omitempty"`
	FinalResponse            string                         `json:"finalResponse,omitempty"`
	Workspace                string                         `json:"workspace,omitempty"`
	AgentError               string                         `json:"agentError,omitempty"`
	EventErrors              []string                       `json:"eventErrors,omitempty"`
	Evaluation               *officialEval                  `json:"evaluation,omitempty"`
	EvaluationError          string                         `json:"evaluationError,omitempty"`
	SkillCountBefore         int                            `json:"skillCountBefore,omitempty"`
	SkillCountAfter          int                            `json:"skillCountAfter,omitempty"`
	LearnedSkillNames        []string                       `json:"learnedSkillNames,omitempty"`
	Metadata                 map[string]string              `json:"metadata,omitempty"`
	ApprovalGate             *evolution.ApprovalGateMetrics `json:"approvalGate,omitempty"`
}

type officialEval struct {
	Passed   bool         `json:"passed"`
	Status   string       `json:"status"`
	Score    scorePayload `json:"score"`
	Items    []scoreItem  `json:"items,omitempty"`
	Errors   []string     `json:"errors,omitempty"`
	Warnings []string     `json:"warnings,omitempty"`
	Raw      interface{}  `json:"raw,omitempty"`
}

type scorePayload struct {
	Achieved float64 `json:"achieved"`
	Max      float64 `json:"max"`
	Percent  float64 `json:"percent"`
}

type scoreItem struct {
	Name     string  `json:"name"`
	Score    float64 `json:"score"`
	MaxScore float64 `json:"max_score"`
	Status   string  `json:"status"`
	Details  string  `json:"details"`
}

// runStats captures only the agent-side signals collected during run.
// Reviewer-side token usage and the end-to-end aggregate are recorded
// directly on taskRunResult after evaluator + reviewer have completed
// (see runSingleTask), so they do not appear here.
type runStats struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	ToolCalls        []string
	SkillToolCalls   []string
	LoadedSkillNames []string
	ClaimDoneCalled  bool
	SkillToolInvoked bool
	FinalResponse    string
	EventErrors      []string
}

type trackedUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type usageTracker struct {
	mu    sync.Mutex
	usage trackedUsage
}

func (t *usageTracker) add(usage *model.Usage) {
	if t == nil || usage == nil {
		return
	}
	t.mu.Lock()
	t.usage.PromptTokens += usage.PromptTokens
	t.usage.CompletionTokens += usage.CompletionTokens
	t.usage.TotalTokens += usage.TotalTokens
	t.mu.Unlock()
}

func (t *usageTracker) snapshot() trackedUsage {
	if t == nil {
		return trackedUsage{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.usage
}

type trackingModel struct {
	base    model.Model
	tracker *usageTracker
}

func newTrackingModel(base model.Model) (*trackingModel, *usageTracker) {
	tracker := &usageTracker{}
	return &trackingModel{base: base, tracker: tracker}, tracker
}

func (m *trackingModel) GenerateContent(
	ctx context.Context,
	request *model.Request,
) (<-chan *model.Response, error) {
	respCh, err := m.base.GenerateContent(ctx, request)
	if err != nil {
		return nil, err
	}

	out := make(chan *model.Response)
	go func() {
		defer close(out)

		var lastUsage *model.Usage
		for resp := range respCh {
			if resp != nil && resp.Usage != nil {
				usageCopy := *resp.Usage
				lastUsage = &usageCopy
			}
			out <- resp
		}
		if lastUsage != nil {
			m.tracker.add(lastUsage)
		}
	}()
	return out, nil
}

func (m *trackingModel) Info() model.Info {
	return m.base.Info()
}

func main() {
	flag.Parse()

	cfg, err := buildConfig()
	if err != nil {
		log.Fatalf("invalid flags: %v", err)
	}

	tasks, err := collectTasks(cfg)
	if err != nil {
		log.Fatalf("load tasks: %v", err)
	}

	result := &benchmarkResult{
		Timestamp:         time.Now().Format(time.RFC3339),
		RequestedMode:     cfg.Mode,
		Model:             cfg.ModelName,
		ReviewerModel:     cfg.ReviewerModelName,
		Variant:           cfg.Variant,
		SkillCraftRoot:    cfg.SkillCraftRoot,
		TaskRoot:          cfg.TaskRoot,
		BaseTask:          cfg.BaseTask,
		Scales:            reportScales(cfg),
		Tasks:             summarizeTasks(tasks),
		OutputDir:         cfg.OutputDir,
		KeepWorkspaces:    cfg.KeepWorkspaces,
		MaxToolIterations: cfg.MaxToolIterations,
		TaskTimeoutSec:    int(cfg.TaskTimeout.Seconds()),
	}

	log.Printf("SkillCraft root: %s", cfg.SkillCraftRoot)
	log.Printf("Selected %d tasks", len(tasks))
	for i, task := range tasks {
		log.Printf("  %d. %s (%s)", i+1, task.ID, task.Description)
	}

	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		log.Fatalf("create output dir: %v", err)
	}
	flushPartial := func() {
		if writeErr := writeResults(cfg.OutputDir, result); writeErr != nil {
			log.Printf("warning: failed to flush partial results.json: %v", writeErr)
		}
	}

	runBaseline := cfg.Mode == modeBaseline || cfg.Mode == modeCompare
	runEvolution := cfg.Mode == modeEvolution || cfg.Mode == modeCompare

	if runBaseline {
		result.Baseline, err = runModeTasks(cfg, modeBaseline, tasks, func() {
			flushPartial()
		})
		if err != nil {
			flushPartial()
			log.Fatalf("baseline run failed: %v", err)
		}
		flushPartial()
	}

	if runEvolution {
		result.Evolution, err = runModeTasks(cfg, modeEvolution, tasks, func() {
			flushPartial()
		})
		if err != nil {
			flushPartial()
			log.Fatalf("evolution run failed: %v", err)
		}
		flushPartial()
	}

	if result.Baseline != nil && result.Evolution != nil {
		result.Comparison = buildComparison(result.Baseline, result.Evolution)
	}

	if err := writeResults(cfg.OutputDir, result); err != nil {
		log.Fatalf("write results: %v", err)
	}
	if err := writeReport(cfg.OutputDir, result); err != nil {
		log.Fatalf("write report: %v", err)
	}

	log.Printf("Saved benchmark outputs to %s", cfg.OutputDir)
}

func buildConfig() (*benchmarkConfig, error) {
	if strings.TrimSpace(*flagSkillCraftRoot) == "" {
		return nil, errors.New("missing -skillcraft-root or SKILLCRAFT_ROOT")
	}
	skillcraftRoot, err := filepath.Abs(strings.TrimSpace(*flagSkillCraftRoot))
	if err != nil {
		return nil, fmt.Errorf("resolve skillcraft root: %w", err)
	}
	info, err := os.Stat(skillcraftRoot)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("invalid skillcraft root: %s", skillcraftRoot)
	}

	mode := runMode(strings.ToLower(strings.TrimSpace(*flagMode)))
	switch mode {
	case modeBaseline, modeEvolution, modeCompare:
	default:
		return nil, fmt.Errorf("unsupported -mode %q", *flagMode)
	}
	if strings.TrimSpace(*flagModel) == "" {
		return nil, errors.New("missing -model")
	}
	if *flagMaxToolIterations <= 0 {
		return nil, fmt.Errorf("invalid -max-tool-iterations %d", *flagMaxToolIterations)
	}
	if *flagMaxTokens <= 0 {
		return nil, fmt.Errorf("invalid -max-tokens %d", *flagMaxTokens)
	}
	if *flagMCPTimeoutSeconds <= 0 {
		return nil, fmt.Errorf("invalid -mcp-timeout-seconds %d", *flagMCPTimeoutSeconds)
	}
	outputDir, err := filepath.Abs(strings.TrimSpace(*flagOutput))
	if err != nil {
		return nil, fmt.Errorf("resolve output dir: %w", err)
	}

	reviewerModel := strings.TrimSpace(*flagReviewerModel)
	if reviewerModel == "" {
		reviewerModel = strings.TrimSpace(*flagModel)
	}

	cfg := &benchmarkConfig{
		SkillCraftRoot:         skillcraftRoot,
		TaskRoot:               strings.Trim(strings.TrimSpace(*flagTaskRoot), "/"),
		BaseTask:               strings.Trim(strings.TrimSpace(*flagBaseTask), "/"),
		Scales:                 parseCSV(*flagScales),
		ExplicitTasks:          parseCSV(*flagTasks),
		Mode:                   mode,
		ModelName:              strings.TrimSpace(*flagModel),
		ReviewerModelName:      reviewerModel,
		Variant:                strings.TrimSpace(*flagVariant),
		OutputDir:              outputDir,
		MaxToolIterations:      *flagMaxToolIterations,
		MaxTokens:              *flagMaxTokens,
		MCPTimeout:             time.Duration(*flagMCPTimeoutSeconds) * time.Second,
		KeepWorkspaces:         *flagKeepWorkspaces,
		Verbose:                *flagVerbose,
		ReviewerSkillBodyChars: *flagReviewerSkillBodyChars,
		MaxPromptSkills:        *flagMaxPromptSkills,
		EnableApprovalGate:     *flagEnableApprovalGate,
		ApprovalGateShadow:     *flagApprovalGateShadow,
		EffectivenessGate:      *flagEffectivenessGate,
	}
	if *flagTaskTimeoutSeconds > 0 {
		cfg.TaskTimeout = time.Duration(*flagTaskTimeoutSeconds) * time.Second
	}
	if seed := strings.TrimSpace(*flagLoadSkillsFrom); seed != "" {
		abs, err := filepath.Abs(seed)
		if err != nil {
			return nil, fmt.Errorf("resolve load-skills-from: %w", err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("invalid load-skills-from %q: %w", seed, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("load-skills-from must be a directory: %s", abs)
		}
		cfg.LoadSkillsFrom = abs
	}
	if len(cfg.ExplicitTasks) == 0 && cfg.BaseTask == "" {
		return nil, errors.New("set either -tasks or -base-task")
	}
	if len(cfg.ExplicitTasks) > 0 && cfg.BaseTask != "" {
		return nil, errors.New("use either -tasks or -base-task, not both")
	}
	if len(cfg.ExplicitTasks) == 0 && len(cfg.Scales) == 0 {
		return nil, errors.New("missing -scales")
	}
	return cfg, nil
}

func collectTasks(cfg *benchmarkConfig) ([]*taskDefinition, error) {
	var specs []string
	if len(cfg.ExplicitTasks) > 0 {
		specs = append(specs, cfg.ExplicitTasks...)
	} else {
		for _, scale := range cfg.Scales {
			if strings.Contains(cfg.BaseTask, "/") {
				specs = append(specs, filepath.Join(cfg.BaseTask, scale))
				continue
			}
			specs = append(specs, filepath.Join(cfg.TaskRoot, cfg.BaseTask, scale))
		}
	}

	out := make([]*taskDefinition, 0, len(specs))
	for _, spec := range specs {
		task, err := loadTaskDefinition(cfg.SkillCraftRoot, spec)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, nil
}

func loadTaskDefinition(skillcraftRoot, spec string) (*taskDefinition, error) {
	taskDir, err := resolveTaskDir(skillcraftRoot, spec)
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(taskDir, "task_config.json")
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", configPath, err)
	}
	var taskCfg rawTaskConfig
	if err := json.Unmarshal(raw, &taskCfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", configPath, err)
	}

	taskDocPath := filepath.Join(taskDir, "docs", "task.md")
	taskDoc, err := os.ReadFile(taskDocPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", taskDocPath, err)
	}
	agentPromptPath := filepath.Join(taskDir, "docs", "agent_system_prompt.md")
	agentPrompt, _ := readOptionalFile(agentPromptPath)
	evalPath := filepath.Join(taskDir, "evaluation", "main.py")
	if _, err := os.Stat(evalPath); err != nil {
		return nil, fmt.Errorf("missing evaluation script for %s", taskDir)
	}
	initialWorkspace := filepath.Join(taskDir, "initial_workspace")
	hasInitialContent := false
	if info, err := os.Stat(initialWorkspace); err == nil && info.IsDir() {
		hasInitialContent = true
	} else {
		initialWorkspace = ""
	}

	baseTask := strings.TrimSpace(taskCfg.Meta.BaseTask)
	scale := strings.TrimSpace(taskCfg.Meta.ScaleLevel)
	if baseTask == "" {
		baseTask = inferBaseTask(taskDir)
	}
	if scale == "" {
		scale = filepath.Base(taskDir)
	}
	taskID := strings.Trim(baseTask+"/"+scale, "/")

	return &taskDefinition{
		ID:                taskID,
		Name:              strings.TrimSpace(taskCfg.TaskName),
		BaseTask:          baseTask,
		Scale:             scale,
		Difficulty:        strings.TrimSpace(taskCfg.Meta.Difficulty),
		Description:       strings.TrimSpace(taskCfg.Meta.Description),
		Dir:               taskDir,
		TaskDocPath:       taskDocPath,
		TaskDoc:           strings.TrimSpace(string(taskDoc)),
		AgentPromptPath:   agentPromptPath,
		AgentPrompt:       strings.TrimSpace(agentPrompt),
		EvaluationScript:  evalPath,
		InitialWorkspace:  initialWorkspace,
		NeededMCPServers:  append([]string(nil), taskCfg.NeededMCPServers...),
		NeededLocalTools:  append([]string(nil), taskCfg.NeededLocalTools...),
		MaxTurns:          taskCfg.MaxTurns,
		TimeoutSeconds:    taskCfg.Timeout,
		HasInitialContent: hasInitialContent,
	}, nil
}

func resolveTaskDir(skillcraftRoot, spec string) (string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", errors.New("empty task spec")
	}

	candidates := make([]string, 0, 4)
	if filepath.IsAbs(spec) {
		candidates = append(candidates, spec)
	} else {
		candidates = append(candidates,
			filepath.Join(skillcraftRoot, spec),
			filepath.Join(skillcraftRoot, "tasks", spec),
			filepath.Join(skillcraftRoot, defaultTaskRoot, spec),
		)
	}

	for _, candidate := range candidates {
		configPath := filepath.Join(candidate, "task_config.json")
		if info, err := os.Stat(configPath); err == nil && !info.IsDir() {
			return filepath.Clean(candidate), nil
		}
	}
	return "", fmt.Errorf("could not resolve task spec %q", spec)
}

func runModeTasks(
	cfg *benchmarkConfig,
	mode runMode,
	tasks []*taskDefinition,
	onProgress func(),
) (*modeResult, error) {
	log.Printf("=== Running %s ===", mode)

	var skillsDir string
	if modeLearnsSkills(mode) {
		skillsDir = filepath.Join(cfg.OutputDir, "managed_skills")
		if err := os.RemoveAll(skillsDir); err != nil {
			return nil, fmt.Errorf("reset managed skills dir: %w", err)
		}
		if err := os.MkdirAll(skillsDir, 0o755); err != nil {
			return nil, fmt.Errorf("create managed skills dir: %w", err)
		}
		if cfg.LoadSkillsFrom != "" {
			n, err := seedManagedSkills(cfg.LoadSkillsFrom, skillsDir)
			if err != nil {
				return nil, fmt.Errorf("seed managed skills from %s: %w", cfg.LoadSkillsFrom, err)
			}
			log.Printf("    seeded %d managed skill(s) from %s", n, cfg.LoadSkillsFrom)
		}
	}

	results := make([]*taskRunResult, 0, len(tasks))
	modeRes := &modeResult{Mode: mode, Cases: results}
	for idx, task := range tasks {
		log.Printf("[%s %d/%d] %s", mode, idx+1, len(tasks), task.ID)
		runResult, err := runSingleTask(cfg, mode, task, idx, skillsDir)
		if err != nil {
			modeRes.Summary = summarizeMode(results, currentSkillNames(mode, skillsDir))
			return modeRes, err
		}
		results = append(results, runResult)
		modeRes.Cases = results
		modeRes.Summary = summarizeMode(results, currentSkillNames(mode, skillsDir))
		if onProgress != nil {
			onProgress()
		}
	}

	modeRes.Summary = summarizeMode(results, currentSkillNames(mode, skillsDir))
	return modeRes, nil
}

// seedManagedSkills copies every immediate subdirectory of srcDir (each
// expected to hold one SKILL.md plus optional sibling files) into dstDir.
// Files at the top level of srcDir are ignored on purpose -- the on-disk
// repository layout used by skill.FSRepository is one folder per skill.
//
// Returns the number of skill folders seeded. Existing folders in dstDir
// with the same name are overwritten so callers can repeatedly seed from
// the same source without manual cleanup.
func seedManagedSkills(srcDir, dstDir string) (int, error) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return 0, fmt.Errorf("read source dir: %w", err)
	}
	var seeded int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())
		if err := os.RemoveAll(dstPath); err != nil {
			return seeded, fmt.Errorf("clear %s: %w", dstPath, err)
		}
		if err := copyDirRecursive(srcPath, dstPath); err != nil {
			return seeded, fmt.Errorf("copy %s -> %s: %w", srcPath, dstPath, err)
		}
		seeded++
	}
	return seeded, nil
}

func copyDirRecursive(srcDir, dstDir string) error {
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dstDir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func currentSkillNames(mode runMode, skillsDir string) []string {
	if !modeLearnsSkills(mode) || skillsDir == "" {
		return nil
	}
	names, err := skillNamesFromDir(skillsDir)
	if err != nil {
		log.Printf("warning: list managed skills: %v", err)
		return nil
	}
	return names
}

func runSingleTask(
	cfg *benchmarkConfig,
	mode runMode,
	task *taskDefinition,
	index int,
	skillsDir string,
) (*taskRunResult, error) {
	workspace := filepath.Join(cfg.OutputDir, "workspaces", string(mode), sanitizeName(task.ID))
	if err := prepareWorkspace(task, workspace); err != nil {
		return nil, fmt.Errorf("prepare workspace for %s: %w", task.ID, err)
	}
	if cfg.Verbose {
		files, _ := listRelativeFiles(workspace)
		log.Printf("  workspace: %s", workspace)
		if len(files) > 0 {
			log.Printf("  initial files: %s", strings.Join(files, ", "))
		}
	}

	result := &taskRunResult{
		TaskID:    task.ID,
		TaskName:  task.Name,
		BaseTask:  task.BaseTask,
		Scale:     task.Scale,
		Mode:      mode,
		Status:    "ok",
		Workspace: workspace,
		Metadata: map[string]string{
			"taskDir": task.Dir,
		},
	}

	start := time.Now()

	var (
		skillRepo        *skill.FSRepository
		skillNamesBefore []string
	)
	if modeLearnsSkills(mode) {
		if err := os.MkdirAll(skillsDir, 0o755); err != nil {
			return nil, fmt.Errorf("create skills dir: %w", err)
		}
		skillRepo, _ = skill.NewFSRepository(skillsDir)
		if skillRepo != nil {
			skillNamesBefore = summariesToNames(skillRepo.Summaries())
			result.SkillCountBefore = len(skillNamesBefore)
			result.HadAvailableSkills = result.SkillCountBefore > 0
		}
	}

	// The evolution service is constructed in the caller (not in
	// executeTask + runner.WithEvolutionService) so we can call
	// EnqueueLearningJob ourselves after the evaluator runs and pass
	// the verdict in via LearningJob.Outcome. The runner-driven hook
	// is intentionally not used in benchmark mode because it would
	// fire-and-forget without an outcome, and the reviewer would then
	// "imagine" the outcome from the transcript alone (see
	// .vscode/plan-evolution-memory.md P0-3 for the rationale).
	sessionService := sessioninmemory.NewSessionService()
	const benchmarkAppName = "skillcraft-benchmark"
	const benchmarkUserID = "skillcraft-benchmark-user"
	sessionID := fmt.Sprintf("%s-%s-%d-%d",
		mode, sanitizeName(task.ID), index, time.Now().UnixNano())

	var (
		evoSvc          evolution.Service
		reviewerTracker *usageTracker
	)
	if modeLearnsSkills(mode) {
		reviewerBaseModel := newOpenAIModel(cfg.ReviewerModelName, cfg.Variant)
		reviewerModel, tracker := newTrackingModel(reviewerBaseModel)
		reviewerTracker = tracker
		evoOpts := []evolution.Option{
			evolution.WithManagedSkillsDir(skillsDir),
			evolution.WithSkillRepository(skillRepo),
			evolution.WithReviewerOptions(
				evolution.WithMessageContentMaxChars(2000),
			),
		}
		if cfg.ReviewerSkillBodyChars != 0 {
			evoOpts = append(evoOpts,
				evolution.WithExistingSkillBodyMaxChars(cfg.ReviewerSkillBodyChars),
			)
		}
		if cfg.EnableApprovalGate {
			// Keep the revision store next to the live managed skills
			// dir so operators can inspect both trees side by side.
			revRoot := filepath.Join(cfg.OutputDir, "managed_skills_revisions")
			evoOpts = append(evoOpts,
				evolution.WithCandidateStore(evolution.NewFileCandidateStore(revRoot)),
				evolution.WithActivePointer(evolution.NewFileActivePointer(revRoot)),
				evolution.WithSpecGate(evolution.NewDefaultSpecGate()),
				evolution.WithSafetyGate(evolution.NewDefaultSafetyGate()),
			)
			if cfg.EffectivenessGate {
				evoOpts = append(evoOpts,
					evolution.WithEffectivenessGate(evolution.NewOutcomeBasedEffectivenessGate()),
				)
			}
			if cfg.ApprovalGateShadow {
				evoOpts = append(evoOpts, evolution.WithApprovalGateShadow(true))
			}
		}
		evoSvc = evolution.NewService(reviewerModel, evoOpts...)
	}

	stats, runErr := executeTask(cfg, task, mode, workspace, skillRepo,
		sessionService, benchmarkAppName, benchmarkUserID, sessionID)
	result.DurationSeconds = round2(time.Since(start).Seconds())
	if stats != nil {
		result.PromptTokens = stats.PromptTokens
		result.CompletionTokens = stats.CompletionTokens
		result.TotalTokens = stats.TotalTokens
		result.ToolCalls = stats.ToolCalls
		result.SkillToolCalls = stats.SkillToolCalls
		result.LoadedSkillNames = stats.LoadedSkillNames
		result.ClaimDoneCalled = stats.ClaimDoneCalled
		result.SkillToolInvoked = stats.SkillToolInvoked
		result.FinalResponse = stats.FinalResponse
		result.EventErrors = stats.EventErrors
	}
	if runErr != nil {
		result.Status = "agent_error"
		result.AgentError = runErr.Error()
		log.Printf("  agent error: %v", runErr)
	}

	eval, evalErr := evaluateTask(cfg, task, workspace)
	if eval != nil {
		result.Evaluation = eval
		if eval.Passed {
			log.Printf("  evaluation: pass (%.1f%%)", eval.Score.Percent)
		} else {
			log.Printf("  evaluation: %s (%.1f%%)", eval.Status, eval.Score.Percent)
		}
		if eval.Passed {
			result.Status = resultStatusFromEvaluation(eval)
		} else if result.Status == "ok" {
			result.Status = resultStatusFromEvaluation(eval)
		}
	}
	if evalErr != nil {
		if result.Status == "ok" {
			result.Status = "evaluation_error"
		}
		result.EvaluationError = evalErr.Error()
		log.Printf("  evaluation error: %v", evalErr)
	}

	// Hand the session + outcome to the reviewer. We do this AFTER the
	// evaluator has run so the reviewer prompt always sees the verdict;
	// then close the service synchronously so the async worker has
	// drained before we measure learned skills on disk.
	if evoSvc != nil {
		enqueueEvolutionWithOutcome(
			sessionService, evoSvc,
			benchmarkAppName, benchmarkUserID, sessionID,
			runErr, eval, evalErr,
		)
		// Capture the optional metrics provider before Close, then read
		// it after Close has drained the async queue.
		var approvalMetrics evolution.ApprovalGateMetricsProvider
		if provider, ok := evoSvc.(evolution.ApprovalGateMetricsProvider); ok && cfg.EnableApprovalGate {
			approvalMetrics = provider
		}
		if err := evoSvc.Close(); err != nil {
			log.Printf("  evolution close error: %v", err)
		}
		if approvalMetrics != nil {
			snap := approvalMetrics.ApprovalGateMetrics()
			result.ApprovalGate = &snap
		}
		if reviewerTracker != nil {
			reviewerUsage := reviewerTracker.snapshot()
			result.ReviewerPromptTokens = reviewerUsage.PromptTokens
			result.ReviewerCompletionTokens = reviewerUsage.CompletionTokens
			result.ReviewerTotalTokens = reviewerUsage.TotalTokens
		}
	}
	result.EndToEndTotalTokens = result.TotalTokens + result.ReviewerTotalTokens

	if modeLearnsSkills(mode) {
		namesAfter, err := skillNamesFromDir(skillsDir)
		if err != nil {
			return nil, fmt.Errorf("list learned skills: %w", err)
		}
		result.SkillCountAfter = len(namesAfter)
		result.LearnedSkillNames = diffStrings(skillNamesBefore, namesAfter)
		if len(result.LearnedSkillNames) > 0 {
			log.Printf("  learned skills: %s", strings.Join(result.LearnedSkillNames, ", "))
		}
	}

	if !cfg.KeepWorkspaces {
		if err := os.RemoveAll(workspace); err != nil {
			return nil, fmt.Errorf("cleanup workspace %s: %w", workspace, err)
		}
		result.Workspace = ""
	}

	return result, nil
}

// enqueueEvolutionWithOutcome fetches the recorded session and submits a
// learning job to the reviewer with an explicit Outcome derived from the
// evaluator. Failures here are logged but never abort the benchmark run --
// missing the reviewer pass is much less bad than losing the per-task
// metrics we already collected.
func enqueueEvolutionWithOutcome(
	sessionService *sessioninmemory.SessionService,
	evoSvc evolution.Service,
	appName, userID, sessionID string,
	runErr error,
	eval *officialEval,
	evalErr error,
) {
	ctx := context.Background()
	sess, err := sessionService.GetSession(ctx, session.Key{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		log.Printf("  evolution enqueue skipped: get session: %v", err)
		return
	}
	if sess == nil {
		log.Printf("  evolution enqueue skipped: session %s not found", sessionID)
		return
	}
	outcome := outcomeFromEval(runErr, eval, evalErr)
	if err := evoSvc.EnqueueLearningJob(ctx, evolution.LearningJob{
		Session: sess,
		Outcome: outcome,
	}); err != nil {
		log.Printf("  evolution enqueue error: %v", err)
	}
}

// outcomeFromEval translates the benchmark's per-task signals (agent run
// error, official evaluator verdict, evaluator runtime error) into the
// generic Outcome the reviewer consumes. Notes are kept short and
// PII-free because the reviewer renders them verbatim into its prompt.
func outcomeFromEval(runErr error, eval *officialEval, evalErr error) *evolution.Outcome {
	out := &evolution.Outcome{Evaluator: "skillcraft"}
	switch {
	case runErr != nil:
		out.Status = evolution.OutcomeAgentError
		out.Notes = truncateOutcomeNote("agent error: " + runErr.Error())
	case evalErr != nil:
		out.Status = evolution.OutcomeAgentError
		out.Notes = truncateOutcomeNote("evaluator runtime error: " + evalErr.Error())
	case eval == nil:
		out.Status = evolution.OutcomeUnknown
		out.Notes = "evaluator did not return a verdict"
	case eval.Passed:
		out.Status = evolution.OutcomeSuccess
		score := eval.Score.Percent
		out.Score = &score
		if status := strings.ToLower(strings.TrimSpace(eval.Status)); status == "partial" {
			out.Status = evolution.OutcomePartial
			out.Notes = truncateOutcomeNote(joinScoreNotes(eval))
		}
	default:
		out.Status = evolution.OutcomeFail
		score := eval.Score.Percent
		out.Score = &score
		out.Notes = truncateOutcomeNote(joinScoreNotes(eval))
	}
	return out
}

// joinScoreNotes builds a short evaluator note from the official eval
// payload, focusing on signals the reviewer can act on (errors first,
// then warnings, then per-item failures).
func joinScoreNotes(eval *officialEval) string {
	if eval == nil {
		return ""
	}
	var parts []string
	for _, e := range eval.Errors {
		if e = strings.TrimSpace(e); e != "" {
			parts = append(parts, e)
		}
	}
	for _, w := range eval.Warnings {
		if w = strings.TrimSpace(w); w != "" {
			parts = append(parts, "warn: "+w)
		}
	}
	for _, item := range eval.Items {
		status := strings.ToLower(strings.TrimSpace(item.Status))
		if status == "" || status == "pass" || status == "passed" {
			continue
		}
		details := strings.TrimSpace(item.Details)
		if details == "" {
			details = strings.TrimSpace(item.Name)
		}
		if details == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", item.Name, details))
	}
	return strings.Join(parts, "; ")
}

// truncateOutcomeNote caps the note string so a noisy evaluator cannot
// blow up the reviewer prompt. The reviewer reads the verdict status
// for routing; the notes are advisory.
func truncateOutcomeNote(s string) string {
	const maxLen = 480
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func executeTask(
	cfg *benchmarkConfig,
	task *taskDefinition,
	mode runMode,
	workspace string,
	repo *skill.FSRepository,
	sessionService *sessioninmemory.SessionService,
	appName, userID, sessionID string,
) (*runStats, error) {
	modelInstance := newOpenAIModel(cfg.ModelName, cfg.Variant)
	stats := &runStats{}
	var availableSkills []string

	localTools, err := newLocalBridgeToolSet(cfg, task, workspace)
	if err != nil {
		return nil, fmt.Errorf("init local bridge: %w", err)
	}
	defer localTools.Close()

	filesystemTools, err := newFilesystemToolSet(cfg, workspace)
	if err != nil {
		return nil, fmt.Errorf("init filesystem mcp: %w", err)
	}
	defer filesystemTools.Close()

	hasSkills := repo != nil && len(repo.Summaries()) > 0
	if hasSkills {
		availableSkills = summariesToNames(repo.Summaries())
	}
	maxTokens := cfg.MaxTokens
	if taskNeedsLowerCompletionBudget(task) && maxTokens > 2048 {
		maxTokens = 2048
	}
	genConfig := model.GenerationConfig{
		MaxTokens: intPtr(maxTokens),
		Stream:    false,
	}

	agentOpts := []llmagent.Option{
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription("SkillCraft benchmark agent"),
		llmagent.WithInstruction(buildInstruction(task, workspace, availableSkills)),
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithToolSets([]tool.ToolSet{localTools, filesystemTools}),
		llmagent.WithMaxToolIterations(cfg.MaxToolIterations),
		llmagent.WithEnableContextCompaction(true),
		llmagent.WithContextCompactionKeepRecentRequests(0),
		llmagent.WithContextCompactionToolResultMaxTokens(128),
		llmagent.WithContextCompactionOversizedToolResultMaxTokens(1024),
	}
	if task.MaxTurns > 0 {
		agentOpts = append(agentOpts, llmagent.WithMaxLLMCalls(task.MaxTurns))
	}
	if hasSkills {
		agentOpts = append(agentOpts,
			llmagent.WithSkills(repo),
			llmagent.WithAllowedSkillTools(llmagent.SkillToolLoad),
			llmagent.WithSkillLoadMode(llmagent.SkillLoadModeOnce),
			llmagent.WithSkillsLoadedContentInToolResults(true),
			llmagent.WithMaxLoadedSkills(1),
		)
		if cfg.MaxPromptSkills > 0 {
			agentOpts = append(agentOpts,
				llmagent.WithMaxOverviewSkills(cfg.MaxPromptSkills),
			)
		}
	}

	agentInstance := llmagent.New("skillcraft-bench-agent", agentOpts...)

	// The reviewer/evolution wiring is owned by runSingleTask now: it
	// constructs evoSvc, calls EnqueueLearningJob with an Outcome
	// AFTER the evaluator runs, and Closes the service to drain the
	// async worker before measuring learned skills on disk. Keeping the
	// runner ignorant of evolution avoids the auto-enqueue path that
	// would fire here without an outcome.
	run := runner.NewRunner(appName, agentInstance,
		runner.WithSessionService(sessionService),
	)
	runClosed := false
	defer func() {
		if !runClosed {
			_ = run.Close()
		}
	}()

	taskTimeout := task.TimeoutSeconds
	if cfg.TaskTimeout > 0 {
		taskTimeout = int(cfg.TaskTimeout.Seconds())
	}
	if taskTimeout <= 0 {
		taskTimeout = defaultFallbackTaskLimit
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(taskTimeout)*time.Second)
	defer cancel()

	userPrompt := buildUserPrompt(task, workspace, availableSkills)

	eventCh, err := run.Run(ctx, userID, sessionID, model.NewUserMessage(userPrompt))
	if err != nil {
		return nil, err
	}

	stats = consumeEvents(eventCh)
	if err := run.Close(); err != nil {
		return stats, err
	}
	runClosed = true
	if ctx.Err() != nil {
		return stats, ctx.Err()
	}
	if len(stats.EventErrors) > 0 {
		return stats, fmt.Errorf(strings.Join(stats.EventErrors, "; "))
	}
	return stats, nil
}

func newLocalBridgeToolSet(
	cfg *benchmarkConfig,
	task *taskDefinition,
	workspace string,
) (*mcpcfg.ToolSet, error) {
	bridgePath, err := bridgeScriptPath()
	if err != nil {
		return nil, err
	}
	args := []string{
		"run",
		"--project", cfg.SkillCraftRoot,
		"python",
		bridgePath,
		"--skillcraft-root", cfg.SkillCraftRoot,
		"--workspace", workspace,
	}
	for _, toolset := range task.NeededLocalTools {
		args = append(args, "--toolset", toolset)
	}

	toolSet := mcpcfg.NewMCPToolSet(mcpcfg.ConnectionConfig{
		Transport: "stdio",
		Command:   "uv",
		Args:      args,
		Timeout:   cfg.MCPTimeout,
	})
	if err := toolSet.Init(context.Background()); err != nil {
		return nil, err
	}
	return toolSet, nil
}

func newFilesystemToolSet(
	cfg *benchmarkConfig,
	workspace string,
) (*mcpcfg.ToolSet, error) {
	toolSet := mcpcfg.NewMCPToolSet(mcpcfg.ConnectionConfig{
		Transport: "stdio",
		Command:   "npx",
		Args: []string{
			"-y",
			"@modelcontextprotocol/server-filesystem",
			workspace,
		},
		Timeout: cfg.MCPTimeout,
	})
	if err := toolSet.Init(context.Background()); err != nil {
		return nil, err
	}
	return toolSet, nil
}

func evaluateTask(
	cfg *benchmarkConfig,
	task *taskDefinition,
	workspace string,
) (*officialEval, error) {
	cmd := exec.Command(
		"uv",
		"run",
		"--project", cfg.SkillCraftRoot,
		"python",
		task.EvaluationScript,
		"--agent_workspace", workspace,
		"--groundtruth_workspace", task.Dir,
		"--res_log_file", "",
		"--launch_time", time.Now().Format(time.RFC3339),
	)
	cmd.Dir = cfg.SkillCraftRoot
	output, err := cmd.CombinedOutput()

	eval, parseErr := parseEvaluationOutput(output)
	if parseErr != nil {
		if err != nil {
			return nil, fmt.Errorf("evaluation command failed: %w; output: %s", err, strings.TrimSpace(string(output)))
		}
		return nil, parseErr
	}
	if err != nil {
		return eval, nil
	}
	return eval, nil
}

func parseEvaluationOutput(output []byte) (*officialEval, error) {
	re := regexp.MustCompile(`(?s)=== SCORE_JSON_START ===\s*(\{.*?\})\s*=== SCORE_JSON_END ===`)
	matches := re.FindSubmatch(output)
	if len(matches) != 2 {
		return nil, errors.New("could not locate SCORE_JSON block")
	}

	var eval officialEval
	if err := json.Unmarshal(matches[1], &eval); err != nil {
		return nil, fmt.Errorf("parse score json: %w", err)
	}
	var raw interface{}
	if err := json.Unmarshal(matches[1], &raw); err == nil {
		eval.Raw = raw
	}
	return &eval, nil
}

func consumeEvents(evtCh <-chan *event.Event) *runStats {
	stats := &runStats{}
	for evt := range evtCh {
		if evt == nil {
			continue
		}
		if evt.Error != nil {
			if strings.TrimSpace(evt.Error.Message) != "" {
				stats.EventErrors = append(stats.EventErrors, evt.Error.Message)
			}
			continue
		}
		if evt.Response == nil {
			continue
		}
		if evt.Response.Usage != nil {
			stats.PromptTokens += evt.Response.Usage.PromptTokens
			stats.CompletionTokens += evt.Response.Usage.CompletionTokens
			stats.TotalTokens += evt.Response.Usage.TotalTokens
		}
		for _, choice := range evt.Response.Choices {
			if len(choice.Message.ToolCalls) > 0 {
				for _, call := range choice.Message.ToolCalls {
					rawName := strings.TrimSpace(call.Function.Name)
					if rawName == "" {
						continue
					}
					normalizedName := normalizeToolCallName(rawName)
					stats.ToolCalls = append(stats.ToolCalls, rawName)
					if normalizedName == "local-claim_done" {
						stats.ClaimDoneCalled = true
					}
					if isSkillToolName(normalizedName) {
						stats.SkillToolInvoked = true
						stats.SkillToolCalls = appendUniqueString(stats.SkillToolCalls, normalizedName)
						if normalizedName == "skill_load" {
							if skillName := extractLoadedSkillName(call.Function.Arguments); skillName != "" {
								stats.LoadedSkillNames = appendUniqueString(stats.LoadedSkillNames, skillName)
							}
						}
					}
				}
			}
			if choice.Message.Role == model.RoleAssistant &&
				strings.TrimSpace(choice.Message.Content) != "" {
				stats.FinalResponse = strings.TrimSpace(choice.Message.Content)
			}
			if choice.Delta.Content != "" {
				stats.FinalResponse = strings.TrimSpace(stats.FinalResponse + choice.Delta.Content)
			}
		}
	}
	return stats
}

func normalizeToolCallName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "mcp_")
	return name
}

func isSkillToolName(name string) bool {
	return strings.HasPrefix(name, "skill_")
}

func extractLoadedSkillName(arguments []byte) string {
	var payload struct {
		Skill string `json:"skill"`
	}
	if err := json.Unmarshal(arguments, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Skill)
}

func buildInstruction(task *taskDefinition, workspace string, availableSkills []string) string {
	parts := []string{
		"You are solving one SkillCraft benchmark task in a single uninterrupted session.",
	}
	if task.AgentPrompt != "" {
		parts = append(parts, task.AgentPrompt)
	}
	parts = append(parts,
		"There will be no follow-up user clarifications. Complete the task autonomously.",
		"Inspect available tools before assuming arguments, and save every required output file into the workspace before you finish.",
		"The task docs may mention filesystem tools using names like filesystem-write_file or filesystem-read_file. In this runtime, use the actual filesystem MCP tool names that are exposed to you, such as write_file, read_file, list_directory, edit_file, create_directory, move_file, search_files, get_file_info, and list_allowed_directories.",
		"SkillCraft local tools keep their actual names, which usually start with local-.",
		"Once a tool call has returned a usable result for given arguments, do not call the same tool with the same arguments again. Move on to the next required step. Re-call only if the earlier call returned an error or was clearly incomplete.",
		fmt.Sprintf("Your workspace directory is %s.", workspace),
	)
	if task.HasInitialContent {
		parts = append(parts,
			"Initial workspace files may be helper inputs, draft plans, or candidate pools. They are not authoritative when the task specification gives an exact list, exact count, exact tool order, or exact output contract.",
		)
	}
	if entities := extractTaskEntities(task.TaskDoc); entities != nil {
		parts = append(parts,
			fmt.Sprintf(
				"The task specification requires exactly these %s: %s.",
				entities.Label,
				strings.Join(entities.Values, ", "),
			),
			fmt.Sprintf(
				"Do not add extra %s from initial workspace files, helper plans, or your own substitutions.",
				entities.Label,
			),
			"If any workspace file contains a larger candidate list, filter it down to the exact task-specified set before making tool calls or writing outputs.",
		)
	}
	if outputFile := extractRequiredOutputFile(task.TaskDoc); outputFile != "" {
		parts = append(parts,
			fmt.Sprintf("Required final deliverable: %s. Helper or plan files are not substitutes.", outputFile),
			fmt.Sprintf("Save it by calling local-write_final_json with {\"path\":\"%s\",\"content\":<raw JSON text>}. Fall back to write_file only if that tool errors.", outputFile),
			"In content, put raw JSON text. Do not JSON-encode the document as a quoted string, do not escape every newline as \\n, do not wrap in markdown fences.",
			fmt.Sprintf("Never end your turn with the final JSON only inside an assistant message. The JSON must be persisted to %s via a tool call before you call local-claim_done.", outputFile),
		)
	}
	if len(availableSkills) > 0 {
		parts = append(parts,
			fmt.Sprintf(
				"Managed skills from earlier tasks are available through skill_load (%d currently visible in the system skill overview).",
				len(availableSkills),
			),
			"Mandatory skill-first protocol: BEFORE any domain tool call (weather_*, mealdb_*, worldbank_*, catfacts_*, pokemon_*, etc.), scan the 'Available skills:' block at the top of the system prompt. If any listed skill name obviously matches the task family (for example a 'Weather' skill for a weather task, a 'Recipe' skill for a cookbook task, an 'Economic' skill for a World Bank task, a 'Cat Facts' skill for a cat breeds task, a 'PokeAPI' or 'Pokedex' skill for a Pokemon task), call the skill_load tool on that skill name as your FIRST tool call. Do this even if you already have a plan. Loading is cheap and the skill body may save many redundant tool calls downstream.",
			"If multiple skills look relevant, pick the most generic one (e.g. 'Multi-City' or 'Multi-Country' variants) and load that one first. Do not load sibling count-specific variants.",
			"After skill_load returns, read the loaded steps and pitfalls and then execute the task. Treat the skill as a reusable checklist, not as the source of truth: the current task specification, required output file, and tool results always override the skill.",
			"Managed skills may come from smaller or earlier tasks and can be incomplete. If the current task needs extra fields, extra steps, or a stricter tool order than the loaded skill mentions, still follow the current task.",
			"If a managed skill mentions a tool that is not in the tool list available for this task, skip that step entirely and use whatever tool the task specification names instead.",
			"After deciding a loaded skill is incomplete or irrelevant, stop reconsidering it and continue with the task using the task specification and tool results.",
			"These managed skills are textual procedures, not prebuilt executable scripts.",
		)
	}
	if taskDocMayContainPreviewMarkers(task.TaskDoc) {
		parts = append(parts,
			"The task docs may abbreviate long literals with trailing `...`. Treat obvious preview markers as formatting unless the task explicitly says the dots are literal input characters.",
		)
	}
	if containsString(task.NeededLocalTools, "claim_done") {
		parts = append(parts,
			"When the required output is saved and checked, call local-claim_done as the final tool.",
			"For JSON outputs, prefer one complete write with write_file when feasible.",
			"Accumulate results in memory while you work, then write the final output once near the end instead of repeatedly rewriting the output file after each subtask.",
			"Do not create draft files, scratch files, or auxiliary reports unless the task explicitly asks for them. Focus on producing the required final output file.",
			"Use local-file_append or local-file_write_json_chunk only when a single complete write is impractical, and make sure the final saved file is valid JSON before calling local-claim_done.",
		)
	}
	if outputFile := extractRequiredOutputFile(task.TaskDoc); outputFile != "" {
		parts = append(parts,
			fmt.Sprintf("Before calling local-claim_done, verify that %s exists in the workspace and contains the complete final JSON.", outputFile),
		)
	}
	if taskNeedsWorkingNotes(task) {
		parts = append(parts,
			"For larger multi-entity tasks, do not rely on raw tool outputs staying in context forever.",
			"After finishing one entity, you may keep a single compact helper JSON file such as working_notes.json with only derived summaries and required fields for completed entities.",
			"If you use working_notes.json, keep it compact, rewrite it with valid JSON, and read it back later instead of depending on full earlier tool outputs.",
			"Do not store raw arrays or raw tool dumps in helper notes, and do not treat helper notes as the final deliverable.",
		)
	}
	return strings.Join(parts, "\n\n")
}

func buildUserPrompt(task *taskDefinition, workspace string, availableSkills []string) string {
	var b strings.Builder
	b.WriteString("Complete the following SkillCraft task.\n\n")
	b.WriteString("## Workspace\n")
	fmt.Fprintf(&b, "- directory: %s\n", workspace)
	files, _ := listRelativeFiles(workspace)
	if len(files) > 0 {
		b.WriteString("- initial files:\n")
		for _, name := range files {
			fmt.Fprintf(&b, "  - %s\n", name)
		}
	}
	if task.HasInitialContent {
		b.WriteString("- note: initial workspace files may contain helper plans or candidate supersets; when the task specification gives an exact list or exact count, the task specification is authoritative.\n")
	}
	if outputFile := extractRequiredOutputFile(task.TaskDoc); outputFile != "" {
		fmt.Fprintf(&b, "- required final output file: %s\n", outputFile)
	}
	b.WriteString("\n## Task Specification\n\n")
	b.WriteString(strings.TrimSpace(task.TaskDoc))
	if entities := extractTaskEntities(task.TaskDoc); entities != nil {
		b.WriteString("\n\n## Exact Required Entities\n")
		fmt.Fprintf(&b, "- %s: %s\n", titleCaseASCII(entities.Label), strings.Join(entities.Values, ", "))
		b.WriteString("- Do not add extra entries from initial workspace files or your own substitutions.\n")
		b.WriteString("- If an initial workspace file contains a larger candidate list, filter it down to this exact set before making tool calls or writing outputs.\n")
	}
	if outputFile := extractRequiredOutputFile(task.TaskDoc); outputFile != "" {
		b.WriteString("\n## Finalization Rules\n")
		fmt.Fprintf(&b, "- Required deliverable: `%s`. Helper or plan files are not substitutes.\n", outputFile)
		fmt.Fprintf(&b, "- Save it via `local-write_final_json` with `{\"path\":\"%s\",\"content\":<raw JSON>}`; fall back to `write_file` only if that tool errors.\n", outputFile)
		b.WriteString("- `content` must be raw JSON text: not a JSON-encoded string, no `\\n`-escaped newlines, no markdown fences.\n")
		fmt.Fprintf(&b, "- Do not end your turn with the final JSON only in chat. The JSON must be persisted to `%s` via a tool call before `local-claim_done`.\n", outputFile)
	}
	if taskNeedsWorkingNotes(task) {
		b.WriteString("\n## Context Management\n")
		b.WriteString("- This is a larger multi-entity task. Raw tool outputs may become too large to keep in context.\n")
		b.WriteString("- After completing an entity, you may keep a single compact helper JSON file such as `working_notes.json` with only derived summaries and required fields.\n")
		b.WriteString("- If you use `working_notes.json`, keep it valid JSON and read it back later instead of relying on full earlier tool outputs.\n")
		b.WriteString("- Do not store raw arrays or raw tool dumps in helper notes.\n")
	}
	if taskDocMayContainPreviewMarkers(task.TaskDoc) {
		b.WriteString("\n\n## Interpretation Note\n")
		b.WriteString("- Long literals in the task doc may be shown as previews ending with `...`; do not assume the dots are literal input characters unless the task explicitly says so.\n")
	}
	if len(availableSkills) > 0 {
		b.WriteString("\n## Managed Skills\n")
		fmt.Fprintf(&b, "- %d managed skill(s) are available through the system skill overview and `skill_load`.\n", len(availableSkills))
		b.WriteString("- Skill-first protocol: if any skill name obviously matches this task family (weather / recipe / cookbook / economic / world bank / cat facts / cat breeds / pokemon / pokedex), call `skill_load` on it BEFORE any domain tool call. This is mandatory when such a skill exists.\n")
		b.WriteString("- If multiple skills look relevant, prefer the most generic one (for example a `Multi-City` or `Multi-Country` variant) over count-specific siblings.\n")
		b.WriteString("- After loading, treat the skill as a reusable checklist. The task specification always overrides the skill when they disagree.\n")
	}
	return b.String()
}

func taskDocMayContainPreviewMarkers(doc string) bool {
	return strings.Contains(doc, "...")
}

func extractRequiredOutputFile(doc string) string {
	re := regexp.MustCompile("Save results to `([^`]+)`")
	matches := re.FindStringSubmatch(doc)
	if len(matches) != 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

type taskEntities struct {
	Label  string
	Values []string
}

func extractTaskEntities(doc string) *taskEntities {
	lines := strings.Split(doc, "\n")
	sectionLabels := map[string]string{
		"cities to analyze":    "cities",
		"countries to analyze": "countries",
		"dishes to include":    "dishes",
	}
	for i, line := range lines {
		header := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "##")))
		label, ok := sectionLabels[header]
		if !ok {
			continue
		}
		values := extractTableColumnValues(lines[i+1:], label)
		if len(values) == 0 {
			return nil
		}
		return &taskEntities{
			Label:  label,
			Values: values,
		}
	}
	return nil
}

func extractTableColumnValues(lines []string, label string) []string {
	targetColumn := map[string]string{
		"cities":    "city",
		"countries": "country",
		"dishes":    "dish",
	}[label]
	if targetColumn == "" {
		return nil
	}
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
			continue
		}
		headerCells := parseMarkdownTableRow(lines[i])
		if len(headerCells) == 0 {
			continue
		}
		targetIdx := indexOfNormalizedCell(headerCells, targetColumn)
		if targetIdx < 0 {
			continue
		}
		var values []string
		for j := i + 1; j < len(lines); j++ {
			row := strings.TrimSpace(lines[j])
			if row == "" || !strings.HasPrefix(row, "|") {
				break
			}
			cells := parseMarkdownTableRow(row)
			if len(cells) == 0 || isMarkdownDividerRow(cells) || targetIdx >= len(cells) {
				continue
			}
			value := strings.TrimSpace(cells[targetIdx])
			if value == "" {
				continue
			}
			values = append(values, value)
		}
		return values
	}
	return nil
}

func parseMarkdownTableRow(line string) []string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	if trimmed == "" {
		return nil
	}
	rawCells := strings.Split(trimmed, "|")
	cells := make([]string, 0, len(rawCells))
	for _, cell := range rawCells {
		cells = append(cells, strings.TrimSpace(cell))
	}
	return cells
}

func isMarkdownDividerRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		if strings.Trim(cell, "-: ") != "" {
			return false
		}
	}
	return true
}

func indexOfNormalizedCell(cells []string, target string) int {
	for i, cell := range cells {
		if strings.EqualFold(strings.TrimSpace(cell), target) {
			return i
		}
	}
	return -1
}

func taskNeedsWorkingNotes(task *taskDefinition) bool {
	if task == nil {
		return false
	}
	if task.MaxTurns >= 100 {
		return true
	}
	if entities := extractTaskEntities(task.TaskDoc); entities != nil && len(entities.Values) >= 5 {
		return true
	}
	lower := strings.ToLower(task.TaskDoc)
	return strings.Contains(lower, "output only summary statistics") ||
		strings.Contains(lower, "summarize large datasets")
}

func taskNeedsLowerCompletionBudget(task *taskDefinition) bool {
	if task == nil {
		return false
	}
	if task.MaxTurns >= 100 {
		return true
	}
	return taskNeedsWorkingNotes(task)
}

func titleCaseASCII(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func reportScales(cfg *benchmarkConfig) []string {
	if cfg == nil || len(cfg.ExplicitTasks) > 0 {
		return nil
	}
	return append([]string(nil), cfg.Scales...)
}

func resultStatusFromEvaluation(eval *officialEval) string {
	if eval == nil {
		return "ok"
	}
	status := strings.ToLower(strings.TrimSpace(eval.Status))
	if eval.Passed {
		if status == "partial" {
			return "partial"
		}
		return "ok"
	}
	if status == "" || status == "pass" {
		return "evaluation_failed"
	}
	return status
}

func summarizeMode(results []*taskRunResult, finalSkillNames []string) *modeSummary {
	summary := &modeSummary{
		Tasks:           len(results),
		FinalSkillNames: finalSkillNames,
		SkillsGenerated: len(finalSkillNames),
	}
	if len(results) == 0 {
		return summary
	}

	for _, res := range results {
		if res.AgentError != "" {
			summary.AgentErrorCount++
		}
		if res.EvaluationError != "" {
			summary.EvalErrorCount++
		}
		summary.ReviewerPromptTokens += res.ReviewerPromptTokens
		summary.ReviewerCompletionTokens += res.ReviewerCompletionTokens
		summary.ReviewerTotalTokens += res.ReviewerTotalTokens
	}

	if overall := summarizeTaskSubset(results); overall != nil {
		summary.Tasks = overall.Tasks
		summary.PassedTasks = overall.PassedTasks
		summary.PassRate = overall.PassRate
		summary.AverageScorePercent = overall.AverageScorePercent
		summary.AverageDurationSec = overall.AverageDurationSec
		summary.AveragePromptTokens = overall.AveragePromptTokens
		summary.AverageOutputTokens = overall.AverageOutputTokens
		summary.AverageTotalTokens = overall.AverageTotalTokens
		summary.AverageReviewerTokens = overall.AverageReviewerTokens
		summary.AverageEndToEndTokens = overall.AverageEndToEndTokens
		summary.ClaimDoneRate = overall.ClaimDoneRate
		summary.SkillsOfferedTasks = overall.SkillsOfferedTasks
		summary.SkillsOfferedRate = overall.SkillsOfferedRate
		summary.SkillToolInvokedTasks = overall.SkillToolInvokedTasks
		summary.SkillToolInvokedRate = overall.SkillToolInvokedRate
	}

	var warmStartResults []*taskRunResult
	var coldStartResults []*taskRunResult
	for _, res := range results {
		if res.HadAvailableSkills {
			warmStartResults = append(warmStartResults, res)
			continue
		}
		coldStartResults = append(coldStartResults, res)
	}
	if len(warmStartResults) > 0 {
		summary.WarmStart = summarizeTaskSubset(warmStartResults)
	}
	if len(coldStartResults) > 0 {
		summary.ColdStart = summarizeTaskSubset(coldStartResults)
	}
	return summary
}

func buildComparison(
	baseline *modeResult,
	evolution *modeResult,
) *compareResult {
	if baseline == nil || evolution == nil ||
		baseline.Summary == nil || evolution.Summary == nil {
		return nil
	}

	comp := &compareResult{
		PassRateDelta:         round2(evolution.Summary.PassRate - baseline.Summary.PassRate),
		ScoreDelta:            round2(evolution.Summary.AverageScorePercent - baseline.Summary.AverageScorePercent),
		DurationDeltaSec:      round2(evolution.Summary.AverageDurationSec - baseline.Summary.AverageDurationSec),
		TokenDelta:            round2(evolution.Summary.AverageTotalTokens - baseline.Summary.AverageTotalTokens),
		EndToEndTokenDelta:    round2(evolution.Summary.AverageEndToEndTokens - baseline.Summary.AverageEndToEndTokens),
		ReviewerTokenDelta:    round2(evolution.Summary.AverageReviewerTokens - baseline.Summary.AverageReviewerTokens),
		ClaimDoneDelta:        round2(evolution.Summary.ClaimDoneRate - baseline.Summary.ClaimDoneRate),
		SkillsOfferedDelta:    round2(evolution.Summary.SkillsOfferedRate - baseline.Summary.SkillsOfferedRate),
		SkillToolInvokedDelta: round2(evolution.Summary.SkillToolInvokedRate - baseline.Summary.SkillToolInvokedRate),
	}
	warmIDs := warmStartTaskIDs(evolution.Cases)
	if len(warmIDs) == 0 {
		return comp
	}

	baselineWarm := summarizeTaskSubset(filterTaskResultsByID(baseline.Cases, warmIDs))
	evolutionWarm := summarizeTaskSubset(filterTaskResultsByID(evolution.Cases, warmIDs))
	if baselineWarm == nil || evolutionWarm == nil {
		return comp
	}

	comp.WarmStartTaskCount = len(warmIDs)
	comp.WarmStartPassRateDelta = round2(evolutionWarm.PassRate - baselineWarm.PassRate)
	comp.WarmStartScoreDelta = round2(evolutionWarm.AverageScorePercent - baselineWarm.AverageScorePercent)
	comp.WarmStartDurationDeltaSec = round2(
		evolutionWarm.AverageDurationSec - baselineWarm.AverageDurationSec,
	)
	comp.WarmStartTokenDelta = round2(
		evolutionWarm.AverageTotalTokens - baselineWarm.AverageTotalTokens,
	)
	comp.WarmStartEndToEndTokenDelta = round2(
		evolutionWarm.AverageEndToEndTokens - baselineWarm.AverageEndToEndTokens,
	)
	return comp
}

func summarizeTaskSubset(results []*taskRunResult) *subsetSummary {
	if len(results) == 0 {
		return nil
	}

	summary := &subsetSummary{Tasks: len(results)}
	var (
		totalScore       float64
		totalDuration    float64
		totalPrompt      float64
		totalCompletion  float64
		totalTokens      float64
		totalReviewer    float64
		totalEndToEnd    float64
		claimDone        int
		skillsOffered    int
		skillToolInvoked int
	)

	for _, res := range results {
		totalDuration += res.DurationSeconds
		totalPrompt += float64(res.PromptTokens)
		totalCompletion += float64(res.CompletionTokens)
		totalTokens += float64(res.TotalTokens)
		totalReviewer += float64(res.ReviewerTotalTokens)
		totalEndToEnd += float64(res.EndToEndTotalTokens)
		if res.ClaimDoneCalled {
			claimDone++
		}
		if res.HadAvailableSkills {
			skillsOffered++
		}
		if res.SkillToolInvoked {
			skillToolInvoked++
		}
		if res.Evaluation != nil {
			totalScore += res.Evaluation.Score.Percent
			if res.Evaluation.Passed {
				summary.PassedTasks++
			}
		}
	}

	count := float64(len(results))
	summary.PassRate = round2(float64(summary.PassedTasks) / count * 100)
	summary.AverageScorePercent = round2(totalScore / count)
	summary.AverageDurationSec = round2(totalDuration / count)
	summary.AveragePromptTokens = round2(totalPrompt / count)
	summary.AverageOutputTokens = round2(totalCompletion / count)
	summary.AverageTotalTokens = round2(totalTokens / count)
	summary.AverageReviewerTokens = round2(totalReviewer / count)
	summary.AverageEndToEndTokens = round2(totalEndToEnd / count)
	summary.ClaimDoneRate = round2(float64(claimDone) / count * 100)
	summary.SkillsOfferedTasks = skillsOffered
	summary.SkillsOfferedRate = round2(float64(skillsOffered) / count * 100)
	summary.SkillToolInvokedTasks = skillToolInvoked
	summary.SkillToolInvokedRate = round2(float64(skillToolInvoked) / count * 100)
	return summary
}

func warmStartTaskIDs(results []*taskRunResult) []string {
	var ids []string
	for _, res := range results {
		if !res.HadAvailableSkills {
			continue
		}
		ids = append(ids, res.TaskID)
	}
	sort.Strings(ids)
	return ids
}

func filterTaskResultsByID(results []*taskRunResult, ids []string) []*taskRunResult {
	if len(ids) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		allowed[id] = struct{}{}
	}
	var out []*taskRunResult
	for _, res := range results {
		if _, ok := allowed[res.TaskID]; ok {
			out = append(out, res)
		}
	}
	return out
}

func writeResults(outputDir string, result *benchmarkResult) error {
	path := filepath.Join(outputDir, "results.json")
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeReport(outputDir string, result *benchmarkResult) error {
	var b strings.Builder
	b.WriteString("# SkillCraft Benchmark Report\n\n")
	fmt.Fprintf(&b, "- Time: `%s`\n", result.Timestamp)
	fmt.Fprintf(&b, "- Requested mode: `%s`\n", result.RequestedMode)
	fmt.Fprintf(&b, "- Model: `%s`\n", result.Model)
	fmt.Fprintf(&b, "- Reviewer model: `%s`\n", result.ReviewerModel)
	if result.BaseTask != "" {
		fmt.Fprintf(&b, "- Base task: `%s`\n", result.BaseTask)
	}
	if len(result.Scales) > 0 {
		fmt.Fprintf(&b, "- Scales: `%s`\n", strings.Join(result.Scales, ","))
	}
	b.WriteString("\n## Task Set\n\n")
	for _, task := range result.Tasks {
		fmt.Fprintf(&b, "- `%s`: %s\n", task.ID, task.Description)
	}

	if result.Baseline != nil {
		appendModeSection(&b, result.Baseline)
	}
	if result.Evolution != nil {
		appendModeSection(&b, result.Evolution)
	}
	if result.Comparison != nil {
		appendComparisonSection(&b, "Comparison (evolution vs. baseline)", result.Comparison)
	}

	return os.WriteFile(filepath.Join(outputDir, "REPORT.md"), []byte(b.String()), 0o644)
}

func appendComparisonSection(b *strings.Builder, title string, c *compareResult) {
	if c == nil {
		return
	}
	fmt.Fprintf(b, "\n## %s\n\n", title)
	fmt.Fprintf(b, "- Overall pass rate delta: %.2f\n", c.PassRateDelta)
	fmt.Fprintf(b, "- Overall score delta: %.2f\n", c.ScoreDelta)
	fmt.Fprintf(b, "- Overall duration delta (s): %.2f\n", c.DurationDeltaSec)
	fmt.Fprintf(b, "- Agent-token delta: %.2f\n", c.TokenDelta)
	fmt.Fprintf(b, "- End-to-end token delta: %.2f\n", c.EndToEndTokenDelta)
	fmt.Fprintf(b, "- Reviewer-token delta: %.2f\n", c.ReviewerTokenDelta)
	fmt.Fprintf(b, "- Claim-done delta: %.2f\n", c.ClaimDoneDelta)
	fmt.Fprintf(b, "- Skills-offered delta: %.2f\n", c.SkillsOfferedDelta)
	fmt.Fprintf(b, "- skill_load-invoked delta: %.2f\n", c.SkillToolInvokedDelta)
	if c.WarmStartTaskCount > 0 {
		fmt.Fprintf(b, "- Warm-start tasks compared: %d\n", c.WarmStartTaskCount)
		fmt.Fprintf(b, "- Warm-start pass rate delta: %.2f\n", c.WarmStartPassRateDelta)
		fmt.Fprintf(b, "- Warm-start score delta: %.2f\n", c.WarmStartScoreDelta)
		fmt.Fprintf(b, "- Warm-start duration delta (s): %.2f\n", c.WarmStartDurationDeltaSec)
		fmt.Fprintf(b, "- Warm-start agent-token delta: %.2f\n", c.WarmStartTokenDelta)
		fmt.Fprintf(b, "- Warm-start end-to-end token delta: %.2f\n", c.WarmStartEndToEndTokenDelta)
	}
}

// approvalGateAggregate sums per-task approval-gate counters so the
// per-mode section in REPORT.md can present a single totals block.
type approvalGateAggregate struct {
	tasks   int
	metrics evolution.ApprovalGateMetrics
}

// aggregateApprovalGate returns a non-nil aggregate only when at least
// one case in the mode observed the approval gate. Returns nil when
// the gate was not enabled for this run so callers can skip the
// section entirely.
func aggregateApprovalGate(cases []*taskRunResult) *approvalGateAggregate {
	var agg approvalGateAggregate
	for _, c := range cases {
		if c == nil || c.ApprovalGate == nil {
			continue
		}
		agg.tasks++
		agg.metrics.CandidatesSeen += c.ApprovalGate.CandidatesSeen
		agg.metrics.RevisionsWritten += c.ApprovalGate.RevisionsWritten
		agg.metrics.SpecGateRejected += c.ApprovalGate.SpecGateRejected
		agg.metrics.SafetyGateRejected += c.ApprovalGate.SafetyGateRejected
		agg.metrics.EffectivenessGateRejected += c.ApprovalGate.EffectivenessGateRejected
		agg.metrics.HumanGateHeld += c.ApprovalGate.HumanGateHeld
		agg.metrics.RevisionsPromoted += c.ApprovalGate.RevisionsPromoted
		agg.metrics.Rollbacks += c.ApprovalGate.Rollbacks
		agg.metrics.DeletionsApplied += c.ApprovalGate.DeletionsApplied
		agg.metrics.UpdatesApplied += c.ApprovalGate.UpdatesApplied
		agg.metrics.CreatesApplied += c.ApprovalGate.CreatesApplied
		agg.metrics.ShadowModeBypassed += c.ApprovalGate.ShadowModeBypassed
	}
	if agg.tasks == 0 {
		return nil
	}
	return &agg
}

func appendModeSection(b *strings.Builder, modeRes *modeResult) {
	if modeRes == nil || modeRes.Summary == nil {
		return
	}
	s := modeRes.Summary
	fmt.Fprintf(b, "\n## %s\n\n", strings.Title(string(modeRes.Mode)))
	fmt.Fprintf(b, "- Tasks: %d\n", s.Tasks)
	fmt.Fprintf(b, "- Passed tasks: %d\n", s.PassedTasks)
	fmt.Fprintf(b, "- Pass rate: %.2f%%\n", s.PassRate)
	fmt.Fprintf(b, "- Average score: %.2f%%\n", s.AverageScorePercent)
	fmt.Fprintf(b, "- Average duration: %.2fs\n", s.AverageDurationSec)
	fmt.Fprintf(b, "- Average agent tokens: %.2f\n", s.AverageTotalTokens)
	fmt.Fprintf(b, "- Average reviewer tokens: %.2f\n", s.AverageReviewerTokens)
	fmt.Fprintf(b, "- Average end-to-end tokens: %.2f\n", s.AverageEndToEndTokens)
	fmt.Fprintf(b, "- Claim-done rate: %.2f%%\n", s.ClaimDoneRate)
	fmt.Fprintf(b, "- Skills-offered rate: %.2f%% (tasks where managed skills were available in the prompt)\n", s.SkillsOfferedRate)
	fmt.Fprintf(b, "- skill_load-invoked rate: %.2f%% (tasks where the agent actually called a skill_* tool)\n", s.SkillToolInvokedRate)
	if len(s.FinalSkillNames) > 0 {
		fmt.Fprintf(b, "- Learned skills: `%s`\n", strings.Join(s.FinalSkillNames, "`, `"))
	}
	if agg := aggregateApprovalGate(modeRes.Cases); agg != nil {
		fmt.Fprintf(b, "- Quality gate (totals across %d task(s)):\n", agg.tasks)
		fmt.Fprintf(b, "  - candidates seen: %d, revisions written: %d, revisions promoted: %d\n",
			agg.metrics.CandidatesSeen, agg.metrics.RevisionsWritten, agg.metrics.RevisionsPromoted)
		fmt.Fprintf(b, "  - spec-gate rejected: %d, safety-gate rejected: %d, effectiveness-gate held: %d, human-gate held: %d, shadow bypassed: %d\n",
			agg.metrics.SpecGateRejected, agg.metrics.SafetyGateRejected, agg.metrics.EffectivenessGateRejected, agg.metrics.HumanGateHeld, agg.metrics.ShadowModeBypassed)
		fmt.Fprintf(b, "  - creates applied: %d, updates applied: %d, deletions applied: %d\n",
			agg.metrics.CreatesApplied, agg.metrics.UpdatesApplied, agg.metrics.DeletionsApplied)
	}
	if s.WarmStart != nil {
		fmt.Fprintf(
			b,
			"- Warm-start subset: %d tasks, %.2f%% score, %.2f agent tokens\n",
			s.WarmStart.Tasks,
			s.WarmStart.AverageScorePercent,
			s.WarmStart.AverageTotalTokens,
		)
	}
	if s.ColdStart != nil {
		fmt.Fprintf(
			b,
			"- Cold-start subset: %d tasks, %.2f%% score, %.2f agent tokens\n",
			s.ColdStart.Tasks,
			s.ColdStart.AverageScorePercent,
			s.ColdStart.AverageTotalTokens,
		)
	}

	b.WriteString("\n| Task | Status | Score | Agent Tokens | End-to-end Tokens | Claim Done | Skills Offered | skill_load Called |\n")
	b.WriteString("|------|--------|------:|-------------:|------------------:|-----------:|---------------:|------------------:|\n")
	for _, res := range modeRes.Cases {
		score := 0.0
		if res.Evaluation != nil {
			score = res.Evaluation.Score.Percent
		}
		claimDone := yesNo(res.ClaimDoneCalled)
		skillsOffered := yesNo(res.HadAvailableSkills)
		skillLoadCalled := yesNo(res.SkillToolInvoked)
		fmt.Fprintf(
			b,
			"| `%s` | `%s` | %.1f | %d | %d | %s | %s | %s |\n",
			res.TaskID,
			res.Status,
			score,
			res.TotalTokens,
			res.EndToEndTotalTokens,
			claimDone,
			skillsOffered,
			skillLoadCalled,
		)
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func prepareWorkspace(task *taskDefinition, workspace string) error {
	if err := os.RemoveAll(workspace); err != nil {
		return err
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return err
	}
	if task.InitialWorkspace == "" {
		return nil
	}
	return copyDir(task.InitialWorkspace, workspace)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func listRelativeFiles(root string) ([]string, error) {
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, rel)
		return nil
	})
	sort.Strings(out)
	return out, err
}

func summariesToNames(summaries []skill.Summary) []string {
	out := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		if strings.TrimSpace(summary.Name) == "" {
			continue
		}
		out = append(out, summary.Name)
	}
	sort.Strings(out)
	return out
}

func skillNamesFromDir(root string) ([]string, error) {
	repo, err := skill.NewFSRepository(root)
	if err != nil {
		return nil, err
	}
	return summariesToNames(repo.Summaries()), nil
}

func summarizeTasks(tasks []*taskDefinition) []*taskSummary {
	out := make([]*taskSummary, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, &taskSummary{
			ID:                task.ID,
			Name:              task.Name,
			BaseTask:          task.BaseTask,
			Scale:             task.Scale,
			Difficulty:        task.Difficulty,
			Description:       task.Description,
			NeededMCPServers:  append([]string(nil), task.NeededMCPServers...),
			NeededLocalTools:  append([]string(nil), task.NeededLocalTools...),
			MaxTurns:          task.MaxTurns,
			TimeoutSeconds:    task.TimeoutSeconds,
			HasInitialContent: task.HasInitialContent,
		})
	}
	return out
}

func newOpenAIModel(name, variant string) model.Model {
	opts := []openai.Option{
		openai.WithEnableTokenTailoring(true),
		openai.WithMaxInputTokens(120000),
	}
	if strings.TrimSpace(variant) != "" {
		opts = append(opts, openai.WithVariant(openai.Variant(variant)))
	}
	return openai.New(name, opts...)
}

func bridgeScriptPath() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("resolve bridge path: runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "bridge", "skillcraft_local_tools_mcp.py"), nil
}

func inferBaseTask(taskDir string) string {
	parent := filepath.Base(filepath.Dir(taskDir))
	if parent != "" && parent != "." && parent != string(filepath.Separator) {
		return parent
	}
	return filepath.Base(taskDir)
}

func readOptionalFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func parseCSV(raw string) []string {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		part = strings.Trim(part, "/")
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func diffStrings(before, after []string) []string {
	set := make(map[string]struct{}, len(before))
	for _, item := range before {
		set[item] = struct{}{}
	}
	var out []string
	for _, item := range after {
		if _, ok := set[item]; ok {
			continue
		}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func appendUniqueString(items []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return items
	}
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func sanitizeName(value string) string {
	value = strings.ReplaceAll(value, "\\", "_")
	value = strings.ReplaceAll(value, "/", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func intPtr(v int) *int {
	return &v
}
