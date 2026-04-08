//
// Tencent is pleased to support the open source community by making
// trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go-benchmark/summary/trpc-agent-go-impl/evaluation/dataset"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	embedopenai "trpc.group/trpc-go/trpc-agent-go/knowledge/embedder/openai"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	sessionpgvector "trpc.group/trpc-go/trpc-agent-go/session/pgvector"
	sessionsummary "trpc.group/trpc-go/trpc-agent-go/session/summary"
)

var (
	flagDatasetFormat = flag.String(
		"dataset-format",
		"",
		"Dataset format: mtbench101 or qmsum (default: auto-detect from -dataset)",
	)
	flagQMSumSplit = flag.String(
		"qmsum-split",
		"test",
		"QMSum split: train, val, or test",
	)
	flagQMSumDomain = flag.String(
		"qmsum-domain",
		"ALL",
		"QMSum domain: ALL, Academic, Committee, or Product",
	)
	flagQMSumQueryType = flag.String(
		"qmsum-query-type",
		"specific",
		"QMSum query type: specific, general, or all",
	)
	flagPGVectorDSN = flag.String(
		"pgvector-dsn",
		"",
		"PostgreSQL DSN for QMSum summary/on-demand modes (env PGVECTOR_DSN)",
	)
	flagEmbedModel = flag.String(
		"embed-model",
		"",
		"Embedding model for QMSum session pgvector indexing (env EMBED_MODEL_NAME or text-embedding-3-small)",
	)
	flagQMSumMaxTokens = flag.Int(
		"qmsum-max-tokens",
		384,
		"Maximum answer tokens for one QMSum query",
	)
	flagQMSumMaxToolIterations = flag.Int(
		"qmsum-max-tool-iterations",
		6,
		"Maximum tool iterations for summary_ondemand mode",
	)
	flagQMSumSummaryWait = flag.Duration(
		"qmsum-summary-wait",
		45*time.Second,
		"Maximum time to wait for session summary generation before querying",
	)
)

const (
	qmsumAppLongContext = "summary-qmsum-long-context"
	qmsumAppSummary     = "summary-qmsum-summary"
	qmsumAppOnDemand    = "summary-qmsum-ondemand"

	qmsumTablePrefix = "summary_qmsum"
)

type qmsumRunMode string

const (
	qmsumModeLongContext qmsumRunMode = "long_context"
	qmsumModeSummary     qmsumRunMode = "summary"
	qmsumModeOnDemand    qmsumRunMode = "summary_ondemand"
)

const qmsumInstruction = `You answer query-based meeting summarization questions from the current session transcript.

Rules:
- Answer the user's query directly and faithfully using only transcript-supported information.
- For a general query, summarize the whole meeting concisely.
- For a specific query, summarize only the requested discussion or point of view.
- Preserve important people, decisions, numbers, dates, and causal relations.
- Prefer one concise paragraph. Do not add bullet points or meta commentary.
- If tools are available and important details may be hidden by session summary, inspect historical events before answering.`

// QMSumModeResult stores one mode's answer, metrics, and token usage.
type QMSumModeResult struct {
	Mode             string        `json:"mode"`
	Answer           string        `json:"answer"`
	Metrics          *QMSumMetrics `json:"metrics,omitempty"`
	TokenUsage       *TokenUsage   `json:"token_usage,omitempty"`
	DurationMs       int64         `json:"duration_ms"`
	SummaryAvailable bool          `json:"summary_available,omitempty"`
	SummaryChars     int           `json:"summary_chars,omitempty"`
	Error            string        `json:"error,omitempty"`
}

// QMSumCaseResult stores the three-mode comparison for one query.
type QMSumCaseResult struct {
	CaseID     string `json:"case_id"`
	MeetingID  string `json:"meeting_id"`
	Domain     string `json:"domain"`
	QueryType  string `json:"query_type"`
	Query      string `json:"query"`
	Reference  string `json:"reference"`
	Turns      int    `json:"turns"`
	Transcript int    `json:"transcript_chars"`

	LongContext *QMSumModeResult `json:"long_context,omitempty"`
	Summary     *QMSumModeResult `json:"summary,omitempty"`
	OnDemand    *QMSumModeResult `json:"summary_ondemand,omitempty"`

	SummaryPromptSavings  float64 `json:"summary_prompt_savings,omitempty"`
	OnDemandPromptSavings float64 `json:"ondemand_prompt_savings,omitempty"`
	OnDemandROUGELGain    float64 `json:"ondemand_rougel_gain,omitempty"`
}

// QMSumAggregate stores averaged metrics for one mode across cases.
type QMSumAggregate struct {
	Count               int           `json:"count"`
	AvgF1               float64       `json:"avg_f1"`
	AvgBLEU             float64       `json:"avg_bleu"`
	AvgROUGE1           float64       `json:"avg_rouge_1"`
	AvgROUGE2           float64       `json:"avg_rouge_2"`
	AvgROUGEL           float64       `json:"avg_rouge_l"`
	AvgLLMScore         float64       `json:"avg_llm_score,omitempty"`
	AvgPromptTokens     float64       `json:"avg_prompt_tokens"`
	AvgCompletionTokens float64       `json:"avg_completion_tokens"`
	AvgTotalTokens      float64       `json:"avg_total_tokens"`
	AvgLatencyMs        float64       `json:"avg_latency_ms"`
	AvgSummaryChars     float64       `json:"avg_summary_chars,omitempty"`
	PromptSavingsVsLong float64       `json:"prompt_savings_vs_long,omitempty"`
	Duration            time.Duration `json:"-"`
}

// QMSumResults is the output JSON payload for QMSum runs.
type QMSumResults struct {
	Timestamp      string             `json:"timestamp"`
	Model          string             `json:"model"`
	DatasetFormat  string             `json:"dataset_format"`
	Dataset        string             `json:"dataset"`
	Split          string             `json:"split"`
	Domain         string             `json:"domain"`
	QueryType      string             `json:"query_type"`
	NumCases       int                `json:"num_cases"`
	EventThreshold int                `json:"event_threshold"`
	Cases          []*QMSumCaseResult `json:"cases"`

	LongContext *QMSumAggregate `json:"long_context,omitempty"`
	Summary     *QMSumAggregate `json:"summary,omitempty"`
	OnDemand    *QMSumAggregate `json:"summary_ondemand,omitempty"`

	OnDemandROUGELGainAvg float64 `json:"ondemand_rouge_l_gain_avg,omitempty"`
}

func detectDatasetFormat(datasetPath string) string {
	switch strings.ToLower(strings.TrimSpace(*flagDatasetFormat)) {
	case "mtbench101":
		return "mtbench101"
	case "qmsum":
		return "qmsum"
	}
	if strings.Contains(strings.ToLower(datasetPath), "qmsum") {
		return "qmsum"
	}
	return "mtbench101"
}

func runQMSumBenchmark(modelName, outputDir string) error {
	log.Printf("=== Summary Evaluation (QMSum) ===")
	log.Printf("Model: %s", modelName)
	log.Printf("Dataset: %s", *flagDataset)
	log.Printf("Split: %s | Domain: %s | QueryType: %s",
		*flagQMSumSplit, *flagQMSumDomain, *flagQMSumQueryType)
	log.Printf("Output: %s", outputDir)
	log.Printf("Event Threshold: %d", *flagEvents)
	log.Printf("LLM Evaluation: %v", *flagUseLLMEval)

	loader := dataset.NewDatasetLoader(*flagDataset)
	cases, err := loader.LoadQMSum(
		*flagQMSumSplit,
		*flagQMSumDomain,
		*flagQMSumQueryType,
	)
	if err != nil {
		return fmt.Errorf("load QMSum: %w", err)
	}
	if *flagNumCases > 0 && *flagNumCases < len(cases) {
		cases = cases[:*flagNumCases]
	}
	log.Printf("Loaded %d QMSum cases", len(cases))

	llm := openai.New(modelName)
	var judge *qmsumLLMJudge
	if *flagUseLLMEval {
		judge = newQMSumLLMJudge(llm)
	}

	longSvc := sessioninmemory.NewSessionService()
	defer func() {
		if err := longSvc.Close(); err != nil {
			log.Printf("close long-context session service: %v", err)
		}
	}()

	summarySvc, err := createQMSumSummaryService(llm)
	if err != nil {
		return err
	}
	defer func() {
		if err := summarySvc.Close(); err != nil {
			log.Printf("close summary session service: %v", err)
		}
	}()

	results := &QMSumResults{
		Timestamp:      time.Now().Format(time.RFC3339),
		Model:          modelName,
		DatasetFormat:  "qmsum",
		Dataset:        *flagDataset,
		Split:          *flagQMSumSplit,
		Domain:         *flagQMSumDomain,
		QueryType:      *flagQMSumQueryType,
		NumCases:       len(cases),
		EventThreshold: *flagEvents,
		Cases:          make([]*QMSumCaseResult, 0, len(cases)),
	}

	start := time.Now()
	completed := make(map[string]bool)
	if *flagResume {
		if checkpoint := loadQMSumCheckpoint(outputDir); checkpoint != nil {
			results = checkpoint
			completed = make(map[string]bool, len(results.Cases))
			for _, cr := range results.Cases {
				completed[cr.CaseID] = true
			}
			log.Printf("Resumed QMSum checkpoint with %d completed cases", len(completed))
		}
	}

	for i, qcase := range cases {
		if completed[qcase.CaseID] {
			log.Printf("[%d/%d] Case %s - SKIPPED", i+1, len(cases), qcase.CaseID)
			continue
		}

		log.Printf("")
		log.Printf("[%d/%d] Case %s (%s/%s)", i+1, len(cases),
			qcase.CaseID, qcase.Domain, qcase.QueryType)

		caseResult, err := evaluateQMSumCase(
			context.Background(),
			llm,
			judge,
			longSvc,
			summarySvc,
			qcase,
		)
		if err != nil {
			log.Printf("  Error: %v", err)
			continue
		}

		results.Cases = append(results.Cases, caseResult)
		saveQMSumCheckpoint(outputDir, results)
		saveQMSumCaseLog(outputDir, caseResult)
		logQMSumCaseResult(caseResult)

		elapsed := time.Since(start)
		avgPerCase := elapsed / time.Duration(len(results.Cases))
		remaining := avgPerCase * time.Duration(len(cases)-i-1)
		log.Printf("  Progress: %d/%d | Elapsed: %v | ETA: %v",
			i+1, len(cases), elapsed.Round(time.Second), remaining.Round(time.Second))
	}

	aggregateQMSumResults(results)
	printQMSumResults(results)
	saveQMSumResults(outputDir, results)
	return nil
}

func createQMSumSummaryService(
	llm model.Model,
) (session.Service, error) {
	dsn := getQMSumPGVectorDSN()
	if dsn == "" {
		return nil, fmt.Errorf(
			"pgvector-dsn or PGVECTOR_DSN is required for QMSum summary benchmark",
		)
	}

	embedModelName := getQMSumEmbedModelName()
	emb := newQMSumEmbeddingEmbedder(embedModelName)
	sum := sessionsummary.NewSummarizer(
		llm,
		sessionsummary.WithChecksAny(
			sessionsummary.CheckEventThreshold(*flagEvents),
		),
	)

	log.Printf(
		"Creating QMSum pgvector session service (embed_model=%s)",
		embedModelName,
	)

	return sessionpgvector.NewService(
		sessionpgvector.WithPostgresClientDSN(dsn),
		sessionpgvector.WithEmbedder(emb),
		sessionpgvector.WithIndexDimension(emb.GetDimensions()),
		sessionpgvector.WithTablePrefix(qmsumTablePrefix),
		sessionpgvector.WithSessionEventLimit(0),
		sessionpgvector.WithSyncIndexing(true),
		sessionpgvector.WithMaxResults(10),
		sessionpgvector.WithSummarizer(sum),
		sessionpgvector.WithAsyncSummaryNum(1),
		sessionpgvector.WithSummaryQueueSize(16),
		sessionpgvector.WithSummaryJobTimeout(*flagQMSumSummaryWait),
	)
}

func evaluateQMSumCase(
	ctx context.Context,
	llm model.Model,
	judge *qmsumLLMJudge,
	longSvc session.Service,
	summarySvc session.Service,
	qcase *dataset.QMSumCase,
) (*QMSumCaseResult, error) {
	if qcase == nil {
		return nil, fmt.Errorf("QMSum case is nil")
	}

	longResult, err := runQMSumMode(
		ctx, llm, judge, longSvc, qcase, qmsumModeLongContext,
	)
	if err != nil {
		return nil, fmt.Errorf("long_context: %w", err)
	}

	summaryResult, err := runQMSumMode(
		ctx, llm, judge, summarySvc, qcase, qmsumModeSummary,
	)
	if err != nil {
		return nil, fmt.Errorf("summary: %w", err)
	}

	onDemandResult, err := runQMSumMode(
		ctx, llm, judge, summarySvc, qcase, qmsumModeOnDemand,
	)
	if err != nil {
		return nil, fmt.Errorf("summary_ondemand: %w", err)
	}

	result := &QMSumCaseResult{
		CaseID:      qcase.CaseID,
		MeetingID:   qcase.MeetingID,
		Domain:      qcase.Domain,
		QueryType:   qcase.QueryType,
		Query:       qcase.Query,
		Reference:   qcase.Answer,
		Turns:       len(qcase.Transcript),
		Transcript:  transcriptCharCount(qcase.Transcript),
		LongContext: longResult,
		Summary:     summaryResult,
		OnDemand:    onDemandResult,
	}
	if longResult != nil && summaryResult != nil &&
		longResult.TokenUsage != nil && summaryResult.TokenUsage != nil &&
		longResult.TokenUsage.PromptTokens > 0 {
		result.SummaryPromptSavings = 100 * float64(
			longResult.TokenUsage.PromptTokens-summaryResult.TokenUsage.PromptTokens,
		) / float64(longResult.TokenUsage.PromptTokens)
	}
	if longResult != nil && onDemandResult != nil &&
		longResult.TokenUsage != nil && onDemandResult.TokenUsage != nil &&
		longResult.TokenUsage.PromptTokens > 0 {
		result.OnDemandPromptSavings = 100 * float64(
			longResult.TokenUsage.PromptTokens-onDemandResult.TokenUsage.PromptTokens,
		) / float64(longResult.TokenUsage.PromptTokens)
	}
	if summaryResult != nil && onDemandResult != nil &&
		summaryResult.Metrics != nil && onDemandResult.Metrics != nil {
		result.OnDemandROUGELGain = onDemandResult.Metrics.ROUGEL -
			summaryResult.Metrics.ROUGEL
	}
	return result, nil
}

func runQMSumMode(
	ctx context.Context,
	llm model.Model,
	judge *qmsumLLMJudge,
	svc session.Service,
	qcase *dataset.QMSumCase,
	mode qmsumRunMode,
) (*QMSumModeResult, error) {
	start := time.Now()
	appName := qmsumAppName(mode)
	userID := qcase.CaseID
	sessionID := fmt.Sprintf("%s-%s", string(mode), qcase.CaseID)

	key := session.Key{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	}
	defer func() {
		_ = svc.DeleteSession(context.Background(), key)
	}()

	sess, err := seedQMSumTranscript(ctx, svc, key, qcase)
	if err != nil {
		return nil, fmt.Errorf("seed transcript: %w", err)
	}

	var (
		summaryText      string
		summaryAvailable bool
	)
	if mode == qmsumModeSummary || mode == qmsumModeOnDemand {
		summaryText, summaryAvailable, err = waitForQMSumSummary(
			ctx, svc, key,
		)
		if err != nil {
			return nil, fmt.Errorf("wait for summary: %w", err)
		}
		// Refresh session before runner starts so invocation sees summaries.
		sess, err = svc.GetSession(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("refresh session: %w", err)
		}
	}
	_ = sess

	ag := newQMSumAgent(llm, mode)
	r := runner.NewRunner(appName, ag, runner.WithSessionService(svc))
	defer r.Close()

	evtCh, err := r.Run(ctx, userID, sessionID, model.NewUserMessage(qcase.Query))
	if err != nil {
		return &QMSumModeResult{
			Mode:             string(mode),
			DurationMs:       time.Since(start).Milliseconds(),
			SummaryAvailable: summaryAvailable,
			SummaryChars:     len(summaryText),
			Error:            err.Error(),
		}, err
	}

	answer, usage := consumeEvents(evtCh)
	return &QMSumModeResult{
		Mode:             string(mode),
		Answer:           strings.TrimSpace(answer),
		Metrics:          evaluateQMSumMetrics(ctx, judge, qcase.Query, qcase.Answer, answer),
		TokenUsage:       usage,
		DurationMs:       time.Since(start).Milliseconds(),
		SummaryAvailable: summaryAvailable,
		SummaryChars:     len(summaryText),
	}, nil
}

func newQMSumAgent(
	llm model.Model,
	mode qmsumRunMode,
) agent.Agent {
	opts := []llmagent.Option{
		llmagent.WithModel(llm),
		llmagent.WithInstruction(qmsumInstruction),
		llmagent.WithGenerationConfig(model.GenerationConfig{
			Stream:      false,
			MaxTokens:   intPtr(*flagQMSumMaxTokens),
			Temperature: float64Ptr(0),
		}),
	}

	if mode == qmsumModeSummary || mode == qmsumModeOnDemand {
		opts = append(opts, llmagent.WithAddSessionSummary(true))
	}
	if mode == qmsumModeOnDemand {
		opts = append(opts,
			llmagent.WithEnableOnDemandSession(true),
			llmagent.WithMaxToolIterations(*flagQMSumMaxToolIterations),
		)
	}

	return llmagent.New("qmsum-eval-agent", opts...)
}

func qmsumAppName(mode qmsumRunMode) string {
	switch mode {
	case qmsumModeSummary:
		return qmsumAppSummary
	case qmsumModeOnDemand:
		return qmsumAppOnDemand
	case qmsumModeLongContext:
		fallthrough
	default:
		return qmsumAppLongContext
	}
}

func seedQMSumTranscript(
	ctx context.Context,
	svc session.Service,
	key session.Key,
	qcase *dataset.QMSumCase,
) (*session.Session, error) {
	_ = svc.DeleteSession(ctx, key)
	sess, err := svc.CreateSession(ctx, key, nil)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	baseTime := time.Now().UTC().Add(-time.Duration(len(qcase.Transcript)) * time.Second)
	for i, turn := range qcase.Transcript {
		content := fmt.Sprintf(
			"[Turn %03d] %s: %s",
			i+1,
			strings.TrimSpace(turn.Speaker),
			strings.TrimSpace(turn.Content),
		)
		// Store transcript turns as user-side history so the seeded session
		// remains visible to backends that expect history to start with a user event.
		evt := event.New(
			fmt.Sprintf("%s-%04d", key.SessionID, i),
			"qmsum-seed",
			event.WithResponse(&model.Response{
				Done: true,
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    model.RoleUser,
							Content: content,
						},
					},
				},
			}),
		)
		evt.Timestamp = baseTime.Add(time.Duration(i) * time.Second)
		if err := svc.AppendEvent(ctx, sess, evt); err != nil {
			return nil, fmt.Errorf("append seed event %d: %w", i, err)
		}
	}
	return sess, nil
}

func waitForQMSumSummary(
	ctx context.Context,
	svc session.Service,
	key session.Key,
) (string, bool, error) {
	deadline := time.Now().Add(*flagQMSumSummaryWait)
	for {
		sess, err := svc.GetSession(ctx, key)
		if err == nil && sess != nil {
			if text, ok := svc.GetSessionSummaryText(ctx, sess); ok && strings.TrimSpace(text) != "" {
				return text, true, nil
			}
		}
		if time.Now().After(deadline) {
			return "", false, nil
		}
		select {
		case <-ctx.Done():
			return "", false, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func transcriptCharCount(turns []dataset.QMSumTranscriptTurn) int {
	total := 0
	for _, turn := range turns {
		total += len(turn.Speaker) + len(turn.Content)
	}
	return total
}

func aggregateQMSumResults(results *QMSumResults) {
	results.LongContext = aggregateQMSumMode(results.Cases, qmsumModeLongContext)
	results.Summary = aggregateQMSumMode(results.Cases, qmsumModeSummary)
	results.OnDemand = aggregateQMSumMode(results.Cases, qmsumModeOnDemand)
	if len(results.Cases) == 0 {
		return
	}

	var totalGain float64
	for _, cr := range results.Cases {
		totalGain += cr.OnDemandROUGELGain
	}
	results.OnDemandROUGELGainAvg = totalGain / float64(len(results.Cases))

	if results.LongContext != nil {
		if results.Summary != nil {
			results.Summary.PromptSavingsVsLong = averagePromptSavings(
				results.Cases,
				func(cr *QMSumCaseResult) float64 { return cr.SummaryPromptSavings },
			)
		}
		if results.OnDemand != nil {
			results.OnDemand.PromptSavingsVsLong = averagePromptSavings(
				results.Cases,
				func(cr *QMSumCaseResult) float64 { return cr.OnDemandPromptSavings },
			)
		}
	}
}

func aggregateQMSumMode(
	cases []*QMSumCaseResult,
	mode qmsumRunMode,
) *QMSumAggregate {
	agg := &QMSumAggregate{}
	for _, cr := range cases {
		var mr *QMSumModeResult
		switch mode {
		case qmsumModeSummary:
			mr = cr.Summary
		case qmsumModeOnDemand:
			mr = cr.OnDemand
		case qmsumModeLongContext:
			fallthrough
		default:
			mr = cr.LongContext
		}
		if mr == nil || mr.Metrics == nil || mr.TokenUsage == nil {
			continue
		}

		agg.Count++
		agg.AvgF1 += mr.Metrics.F1
		agg.AvgBLEU += mr.Metrics.BLEU
		agg.AvgROUGE1 += mr.Metrics.ROUGE1
		agg.AvgROUGE2 += mr.Metrics.ROUGE2
		agg.AvgROUGEL += mr.Metrics.ROUGEL
		agg.AvgLLMScore += mr.Metrics.LLMScore
		agg.AvgPromptTokens += float64(mr.TokenUsage.PromptTokens)
		agg.AvgCompletionTokens += float64(mr.TokenUsage.CompletionTokens)
		agg.AvgTotalTokens += float64(mr.TokenUsage.TotalTokens)
		agg.AvgLatencyMs += float64(mr.DurationMs)
		agg.AvgSummaryChars += float64(mr.SummaryChars)
	}
	if agg.Count == 0 {
		return agg
	}

	n := float64(agg.Count)
	agg.AvgF1 /= n
	agg.AvgBLEU /= n
	agg.AvgROUGE1 /= n
	agg.AvgROUGE2 /= n
	agg.AvgROUGEL /= n
	agg.AvgLLMScore /= n
	agg.AvgPromptTokens /= n
	agg.AvgCompletionTokens /= n
	agg.AvgTotalTokens /= n
	agg.AvgLatencyMs /= n
	agg.AvgSummaryChars /= n
	return agg
}

func averagePromptSavings(
	cases []*QMSumCaseResult,
	getter func(*QMSumCaseResult) float64,
) float64 {
	if len(cases) == 0 {
		return 0
	}
	var total float64
	for _, cr := range cases {
		total += getter(cr)
	}
	return total / float64(len(cases))
}

func printQMSumResults(results *QMSumResults) {
	fmt.Println("\n" + strings.Repeat("=", 72))
	fmt.Println("Summary Evaluation Results (QMSum)")
	fmt.Println(strings.Repeat("=", 72))
	fmt.Printf("Model: %s\n", results.Model)
	fmt.Printf("Cases: %d | Split: %s | Domain: %s | QueryType: %s\n",
		results.NumCases, results.Split, results.Domain, results.QueryType)

	printQMSumAggregate("Long Context", results.LongContext)
	printQMSumAggregate("Summary", results.Summary)
	printQMSumAggregate("Summary On-Demand", results.OnDemand)

	fmt.Printf("\nOn-Demand ROUGE-L Gain vs Summary: %.4f\n", results.OnDemandROUGELGainAvg)
	fmt.Println(strings.Repeat("=", 72))
}

func printQMSumAggregate(title string, agg *QMSumAggregate) {
	if agg == nil || agg.Count == 0 {
		fmt.Printf("\n--- %s ---\n(no successful cases)\n", title)
		return
	}
	fmt.Printf("\n--- %s ---\n", title)
	fmt.Printf("Cases: %d\n", agg.Count)
	fmt.Printf("ROUGE-1/2/L: %.4f / %.4f / %.4f\n",
		agg.AvgROUGE1, agg.AvgROUGE2, agg.AvgROUGEL)
	fmt.Printf("F1/BLEU: %.4f / %.4f\n", agg.AvgF1, agg.AvgBLEU)
	if agg.AvgLLMScore > 0 {
		fmt.Printf("LLM Score: %.4f\n", agg.AvgLLMScore)
	}
	fmt.Printf("Avg Tokens (prompt/completion/total): %.0f / %.0f / %.0f\n",
		agg.AvgPromptTokens, agg.AvgCompletionTokens, agg.AvgTotalTokens)
	fmt.Printf("Avg Latency: %.0f ms\n", agg.AvgLatencyMs)
	if agg.AvgSummaryChars > 0 {
		fmt.Printf("Avg Summary Chars: %.0f\n", agg.AvgSummaryChars)
	}
	if agg.PromptSavingsVsLong != 0 {
		fmt.Printf("Prompt Savings vs Long Context: %.2f%%\n", agg.PromptSavingsVsLong)
	}
}

func logQMSumCaseResult(cr *QMSumCaseResult) {
	log.Printf("  Query: %s", truncateStr(cr.Query, 180))
	if cr.LongContext != nil && cr.LongContext.Metrics != nil && cr.LongContext.TokenUsage != nil {
		log.Printf("  LongContext     R-L=%.4f p=%d c=%d t=%d",
			cr.LongContext.Metrics.ROUGEL,
			cr.LongContext.TokenUsage.PromptTokens,
			cr.LongContext.TokenUsage.CompletionTokens,
			cr.LongContext.TokenUsage.TotalTokens,
		)
	}
	if cr.Summary != nil && cr.Summary.Metrics != nil && cr.Summary.TokenUsage != nil {
		log.Printf("  Summary         R-L=%.4f p=%d c=%d t=%d saved=%.2f%%",
			cr.Summary.Metrics.ROUGEL,
			cr.Summary.TokenUsage.PromptTokens,
			cr.Summary.TokenUsage.CompletionTokens,
			cr.Summary.TokenUsage.TotalTokens,
			cr.SummaryPromptSavings,
		)
	}
	if cr.OnDemand != nil && cr.OnDemand.Metrics != nil && cr.OnDemand.TokenUsage != nil {
		log.Printf("  Summary+OnDemand R-L=%.4f p=%d c=%d t=%d saved=%.2f%% gain=%.4f",
			cr.OnDemand.Metrics.ROUGEL,
			cr.OnDemand.TokenUsage.PromptTokens,
			cr.OnDemand.TokenUsage.CompletionTokens,
			cr.OnDemand.TokenUsage.TotalTokens,
			cr.OnDemandPromptSavings,
			cr.OnDemandROUGELGain,
		)
	}
}

func saveQMSumCaseLog(outputDir string, cr *QMSumCaseResult) {
	path := filepath.Join(outputDir, cr.CaseID+".log")
	f, err := os.Create(path)
	if err != nil {
		log.Printf("create QMSum case log: %v", err)
		return
	}
	defer f.Close()

	writeMode := func(title string, mr *QMSumModeResult) {
		if mr == nil {
			return
		}
		fmt.Fprintf(f, "=== %s ===\n", title)
		fmt.Fprintf(f, "Duration: %dms\n", mr.DurationMs)
		fmt.Fprintf(f, "SummaryAvailable: %v\n", mr.SummaryAvailable)
		fmt.Fprintf(f, "SummaryChars: %d\n", mr.SummaryChars)
		if mr.TokenUsage != nil {
			fmt.Fprintf(f, "Tokens: prompt=%d completion=%d total=%d\n",
				mr.TokenUsage.PromptTokens,
				mr.TokenUsage.CompletionTokens,
				mr.TokenUsage.TotalTokens,
			)
		}
		if mr.Metrics != nil {
			fmt.Fprintf(f, "Metrics: F1=%.4f BLEU=%.4f R1=%.4f R2=%.4f RL=%.4f LLM=%.4f\n",
				mr.Metrics.F1, mr.Metrics.BLEU, mr.Metrics.ROUGE1,
				mr.Metrics.ROUGE2, mr.Metrics.ROUGEL, mr.Metrics.LLMScore,
			)
		}
		if mr.Error != "" {
			fmt.Fprintf(f, "Error: %s\n", mr.Error)
		}
		fmt.Fprintf(f, "Answer:\n%s\n\n", mr.Answer)
	}

	fmt.Fprintf(f, "CaseID: %s\nMeetingID: %s\nDomain: %s\nQueryType: %s\n\n",
		cr.CaseID, cr.MeetingID, cr.Domain, cr.QueryType)
	fmt.Fprintf(f, "Query:\n%s\n\nReference:\n%s\n\n",
		cr.Query, cr.Reference)
	writeMode("LONG CONTEXT", cr.LongContext)
	writeMode("SUMMARY", cr.Summary)
	writeMode("SUMMARY ON-DEMAND", cr.OnDemand)
}

func saveQMSumResults(outputDir string, results *QMSumResults) {
	path := filepath.Join(outputDir, "results.json")
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		log.Printf("marshal QMSum results: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		log.Printf("write QMSum results: %v", err)
		return
	}
	log.Printf("Results saved to: %s", path)
}

func saveQMSumCheckpoint(outputDir string, results *QMSumResults) {
	path := filepath.Join(outputDir, "checkpoint.json")
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		log.Printf("marshal QMSum checkpoint: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		log.Printf("write QMSum checkpoint: %v", err)
	}
}

func loadQMSumCheckpoint(outputDir string) *QMSumResults {
	path := filepath.Join(outputDir, "checkpoint.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var results QMSumResults
	if err := json.Unmarshal(data, &results); err != nil {
		log.Printf("parse QMSum checkpoint: %v", err)
		return nil
	}
	return &results
}

func getQMSumEmbedModelName() string {
	if *flagEmbedModel != "" {
		return *flagEmbedModel
	}
	if env := os.Getenv("EMBED_MODEL_NAME"); env != "" {
		return env
	}
	return "text-embedding-3-small"
}

func newQMSumEmbeddingEmbedder(modelName string) *embedopenai.Embedder {
	opts := []embedopenai.Option{
		embedopenai.WithModel(modelName),
	}
	if apiKey := os.Getenv("OPENAI_EMBEDDING_API_KEY"); apiKey != "" {
		opts = append(opts, embedopenai.WithAPIKey(apiKey))
	}
	baseURL := os.Getenv("OPENAI_EMBEDDING_BASE_URL")
	if baseURL == "" {
		baseURL = os.Getenv("OPENAI_BASE_URL")
	}
	if baseURL != "" {
		opts = append(opts, embedopenai.WithBaseURL(baseURL))
	}
	return embedopenai.New(opts...)
}

func getQMSumPGVectorDSN() string {
	if *flagPGVectorDSN != "" {
		return *flagPGVectorDSN
	}
	return os.Getenv("PGVECTOR_DSN")
}
