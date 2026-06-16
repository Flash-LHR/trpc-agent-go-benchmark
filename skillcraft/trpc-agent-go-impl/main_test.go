package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/evolution"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestConsumeEventsTracksClaimDoneAndSkillToolInvocations(t *testing.T) {
	evtCh := make(chan *event.Event, 1)
	evtCh <- &event.Event{
		Response: &model.Response{
			Usage: &model.Usage{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
			Choices: []model.Choice{
				{
					Message: model.Message{
						ToolCalls: []model.ToolCall{
							{
								Function: model.FunctionDefinitionParam{
									Name:      "mcp_local-claim_done",
									Arguments: []byte(`{}`),
								},
							},
							{
								Function: model.FunctionDefinitionParam{
									Name:      "skill_load",
									Arguments: []byte(`{"skill":"Perform DNA Sequence Analysis"}`),
								},
							},
						},
					},
				},
			},
		},
	}
	close(evtCh)

	stats := consumeEvents(evtCh)
	require.True(t, stats.ClaimDoneCalled)
	require.True(t, stats.SkillToolInvoked)
	require.Equal(t, []string{"skill_load"}, stats.SkillToolCalls)
	require.Equal(t, []string{"Perform DNA Sequence Analysis"}, stats.LoadedSkillNames)
}

func TestBuildComparisonUsesWarmStartSubset(t *testing.T) {
	baselineCases := []*taskRunResult{
		{
			TaskID:              "task-1",
			TotalTokens:         100,
			EndToEndTotalTokens: 100,
			Evaluation:          &officialEval{Passed: true, Score: scorePayload{Percent: 50}},
		},
		{
			TaskID:              "task-2",
			TotalTokens:         200,
			EndToEndTotalTokens: 200,
			Evaluation:          &officialEval{Passed: true, Score: scorePayload{Percent: 60}},
		},
	}
	evolutionCases := []*taskRunResult{
		{
			TaskID:              "task-1",
			TotalTokens:         150,
			ReviewerTotalTokens: 30,
			EndToEndTotalTokens: 180,
			Evaluation:          &officialEval{Passed: true, Score: scorePayload{Percent: 70}},
		},
		{
			TaskID:              "task-2",
			TotalTokens:         120,
			ReviewerTotalTokens: 30,
			EndToEndTotalTokens: 150,
			HadAvailableSkills:  true,
			SkillToolInvoked:    true,
			Evaluation:          &officialEval{Passed: true, Score: scorePayload{Percent: 80}},
		},
	}

	baseline := &modeResult{
		Mode:    modeBaseline,
		Cases:   baselineCases,
		Summary: summarizeMode(baselineCases, nil),
	}
	evolution := &modeResult{
		Mode:    modeEvolution,
		Cases:   evolutionCases,
		Summary: summarizeMode(evolutionCases, []string{"Perform DNA Sequence Analysis"}),
	}

	comp := buildComparison(baseline, evolution)
	require.NotNil(t, comp)
	require.Equal(t, 1, comp.WarmStartTaskCount)
	require.InDelta(t, 20.0, comp.WarmStartScoreDelta, 0.02)
	require.InDelta(t, -80.0, comp.WarmStartTokenDelta, 0.02)
	require.InDelta(t, -50.0, comp.WarmStartEndToEndTokenDelta, 0.02)
	require.InDelta(t, 50.0, comp.SkillsOfferedDelta, 0.02)
	require.InDelta(t, 50.0, comp.SkillToolInvokedDelta, 0.02)
}

func TestBuildInstructionPrioritizesTaskSpecOverSkills(t *testing.T) {
	task := &taskDefinition{
		TaskDoc:          "SEQ_01: ATGC...\n\nSave results to `dna_results.json`:",
		NeededLocalTools: []string{"claim_done"},
	}

	prompt := buildInstruction(task, "/tmp/workspace", []string{"DNA Sequence Analysis Workflow"})

	require.Contains(t, prompt, "Mandatory skill-first protocol")
	require.Contains(t, prompt, "Managed skills from earlier tasks are available through skill_load")
	require.Contains(t, prompt, "skill_load tool on that skill name as your FIRST tool call")
	require.Contains(t, prompt, "Managed skills may come from smaller or earlier tasks and can be incomplete")
	require.Contains(t, prompt, "still follow the current task")
	require.Contains(t, prompt, "stop reconsidering it")
	require.Contains(t, prompt, "trailing `...`")
	require.Contains(t, prompt, "do not call the same tool with the same arguments again")
	require.Contains(t, prompt, "prefer one complete write with write_file")
	require.Contains(t, prompt, "write the final output once near the end")
	require.Contains(t, prompt, "Do not create draft files, scratch files, or auxiliary reports")
	require.Contains(t, prompt, "final saved file is valid JSON")
	require.Contains(t, prompt, "Required final deliverable: dna_results.json")
	require.Contains(t, prompt, "Save it by calling local-write_final_json")
	require.Contains(t, prompt, "{\"path\":\"dna_results.json\",\"content\":<raw JSON text>}")
	require.Contains(t, prompt, "verify that dna_results.json exists")
	require.Contains(t, prompt, "put raw JSON text")
	require.Contains(t, prompt, "do not escape every newline as \\n")
	require.Contains(t, prompt, "Never end your turn with the final JSON only inside an assistant message")
	require.Contains(t, prompt, "If a managed skill mentions a tool that is not in the tool list available for this task, skip that step")
}

func TestBuildInstructionDoesNotMentionSkillManage(t *testing.T) {
	// trpc-agent-go intentionally keeps skill management on the
	// reviewer-driven async path, so the agent prompt must never
	// reference the (deliberately-removed) skill_manage tool.
	task := &taskDefinition{
		TaskDoc:          "SEQ_01: ATGC...\n\nSave results to `dna_results.json`:",
		NeededLocalTools: []string{"claim_done"},
	}

	prompt := buildInstruction(task, "/tmp/workspace", nil)
	require.NotContains(t, prompt, "skill_manage")

	userPrompt := buildUserPrompt(task, "/tmp/workspace", nil)
	require.NotContains(t, userPrompt, "skill_manage")
	require.NotContains(t, userPrompt, "## Skill Authoring")
}

func TestExtractTaskEntitiesParsesPrimaryTaskTable(t *testing.T) {
	taskDoc := `# Task: Weather Monitor (4 Cities × 4 APIs) - M2

## Cities to Analyze

| # | City | Latitude | Longitude |
|---|------|----------|----------|
| 1 | Tokyo | 35.6762 | 139.6503 |
| 2 | New York | 40.7128 | -74.006 |
| 3 | London | 51.5074 | -0.1278 |
| 4 | Sydney | -33.8688 | 151.2093 |

## Summary Requirements

| Data Type | Tool Returns | Required Output |
|-----------|-------------|-----------------|
| Hourly | 168 values | avg, max, min |`

	entities := extractTaskEntities(taskDoc)
	require.NotNil(t, entities)
	require.Equal(t, "cities", entities.Label)
	require.Equal(t, []string{"Tokyo", "New York", "London", "Sydney"}, entities.Values)
}

func TestBuildInstructionEnforcesExactTaskEntitiesOverInitialFiles(t *testing.T) {
	task := &taskDefinition{
		TaskDoc: `## Countries to Analyze

| # | Country | Code | Region |
|---|---------|------|--------|
| 1 | United States | US | North America |
| 2 | China | CHN | East Asia & Pacific |
| 3 | Japan | JPN | East Asia & Pacific |
| 4 | Germany | DEU | Europe & Central Asia |`,
		NeededLocalTools:  []string{"claim_done"},
		HasInitialContent: true,
	}

	prompt := buildInstruction(task, "/tmp/workspace", nil)

	require.Contains(t, prompt, "Initial workspace files may be helper inputs")
	require.Contains(t, prompt, "requires exactly these countries: United States, China, Japan, Germany")
	require.Contains(t, prompt, "Do not add extra countries")
	require.Contains(t, prompt, "filter it down to the exact task-specified set")
}

func TestBuildUserPromptPutsTaskSpecBeforeManagedSkills(t *testing.T) {
	task := &taskDefinition{
		TaskDoc: "SEQ_01: ATGC...",
	}

	prompt := buildUserPrompt(task, "/tmp/workspace", []string{"DNA Sequence Analysis Workflow"})

	require.Contains(t, prompt, "## Task Specification")
	require.Contains(t, prompt, "## Managed Skills")
	require.Less(t,
		strings.Index(prompt, "## Task Specification"),
		strings.Index(prompt, "## Managed Skills"),
	)
	require.Contains(t, prompt, "Skill-first protocol")
	require.Contains(t, prompt, "task specification always overrides the skill")
}

func TestBuildUserPromptIncludesExactTaskEntities(t *testing.T) {
	task := &taskDefinition{
		TaskDoc: `## Dishes to Include

| # | Dish | Search Name | Cuisine |
|---|------|-------------|---------|
| 1 | Spaghetti Carbonara | carbonara | Italian |
| 2 | Tandoori Chicken | tandoori | Indian |
| 3 | Pad Thai | pad thai | Thai |
| 4 | Beef Bourguignon | beef bourguignon | French |
| 5 | Sushi | sushi | Japanese |`,
		HasInitialContent: true,
	}

	prompt := buildUserPrompt(task, "/tmp/workspace", nil)

	require.Contains(t, prompt, "## Exact Required Entities")
	require.Contains(t, prompt, "Dishes: Spaghetti Carbonara, Tandoori Chicken, Pad Thai, Beef Bourguignon, Sushi")
	require.Contains(t, prompt, "Do not add extra entries from initial workspace files")
	require.Contains(t, prompt, "task specification is authoritative")
}

func TestExtractRequiredOutputFile(t *testing.T) {
	taskDoc := "## Required Output\n\nSave results to `weather_report.json`:\n\n```json\n{}"

	require.Equal(t, "weather_report.json", extractRequiredOutputFile(taskDoc))
	require.Equal(t, "", extractRequiredOutputFile("no explicit output file"))
}

func TestBuildUserPromptIncludesFinalizationRules(t *testing.T) {
	task := &taskDefinition{
		TaskDoc: "## Required Output\n\nSave results to `recipe_cookbook.json`:\n\n```json\n{}",
	}

	prompt := buildUserPrompt(task, "/tmp/workspace", nil)

	require.Contains(t, prompt, "## Finalization Rules")
	require.Contains(t, prompt, "Required deliverable: `recipe_cookbook.json`")
	require.Contains(t, prompt, "Save it via `local-write_final_json`")
	require.Contains(t, prompt, "`{\"path\":\"recipe_cookbook.json\",\"content\":<raw JSON>}`")
	require.Contains(t, prompt, "fall back to `write_file` only if that tool errors")
	require.Contains(t, prompt, "must be raw JSON text")
	require.Contains(t, prompt, "Do not end your turn with the final JSON only in chat")
}

func TestBuildInstructionAddsWorkingNotesGuidanceForLargeTasks(t *testing.T) {
	task := &taskDefinition{
		TaskDoc: `## Cities to Analyze

| # | City | Latitude | Longitude |
|---|------|----------|----------|
| 1 | Tokyo | 35.6762 | 139.6503 |
| 2 | New York | 40.7128 | -74.006 |
| 3 | London | 51.5074 | -0.1278 |
| 4 | Sydney | -33.8688 | 151.2093 |
| 5 | Dubai | 25.2048 | 55.2708 |

## Summary Requirements

Output ONLY summary statistics, NOT raw data arrays!`,
		MaxTurns: 100,
	}

	prompt := buildInstruction(task, "/tmp/workspace", nil)

	require.Contains(t, prompt, "do not rely on raw tool outputs staying in context forever")
	require.Contains(t, prompt, "single compact helper JSON file such as working_notes.json")
	require.Contains(t, prompt, "read it back later")
	require.Contains(t, prompt, "Do not store raw arrays or raw tool dumps")
	require.True(t, taskNeedsWorkingNotes(task))
	require.True(t, taskNeedsLowerCompletionBudget(task))
}

func TestResultStatusFromEvaluation(t *testing.T) {
	require.Equal(t, "ok", resultStatusFromEvaluation(nil))
	require.Equal(t, "ok", resultStatusFromEvaluation(&officialEval{Passed: true, Status: "pass"}))
	require.Equal(t, "partial", resultStatusFromEvaluation(&officialEval{Passed: true, Status: "partial"}))
	require.Equal(t, "fail", resultStatusFromEvaluation(&officialEval{Passed: false, Status: "fail"}))
	require.Equal(t, "evaluation_failed", resultStatusFromEvaluation(&officialEval{Passed: false}))
}

func TestOutcomeFromEval(t *testing.T) {
	t.Run("agent error wins over everything", func(t *testing.T) {
		o := outcomeFromEval(errors.New("max tool iterations"), &officialEval{Passed: true}, nil)
		require.Equal(t, evolution.OutcomeAgentError, o.Status)
		require.Contains(t, o.Notes, "max tool iterations")
		require.Equal(t, "skillcraft", o.Evaluator)
	})

	t.Run("evaluator runtime error reported as agent_error", func(t *testing.T) {
		o := outcomeFromEval(nil, nil, errors.New("python missing"))
		require.Equal(t, evolution.OutcomeAgentError, o.Status)
		require.Contains(t, o.Notes, "python missing")
	})

	t.Run("nil eval falls back to unknown", func(t *testing.T) {
		o := outcomeFromEval(nil, nil, nil)
		require.Equal(t, evolution.OutcomeUnknown, o.Status)
		require.NotEmpty(t, o.Notes)
	})

	t.Run("passed=true status=pass becomes success with score", func(t *testing.T) {
		o := outcomeFromEval(nil, &officialEval{
			Passed: true,
			Status: "pass",
			Score:  scorePayload{Percent: 100},
		}, nil)
		require.Equal(t, evolution.OutcomeSuccess, o.Status)
		require.NotNil(t, o.Score)
		require.InDelta(t, 100.0, *o.Score, 1e-9)
	})

	t.Run("passed=true status=partial becomes partial with notes", func(t *testing.T) {
		o := outcomeFromEval(nil, &officialEval{
			Passed: true,
			Status: "partial",
			Score:  scorePayload{Percent: 50},
			Items: []scoreItem{
				{Name: "indicator GDP", Status: "fail", Details: "wrong code"},
			},
		}, nil)
		require.Equal(t, evolution.OutcomePartial, o.Status)
		require.NotNil(t, o.Score)
		require.Contains(t, o.Notes, "indicator GDP")
		require.Contains(t, o.Notes, "wrong code")
	})

	t.Run("passed=false becomes fail with errors notes", func(t *testing.T) {
		o := outcomeFromEval(nil, &officialEval{
			Passed: false,
			Status: "fail",
			Errors: []string{"economic_snapshot.json not found"},
			Score:  scorePayload{Percent: 0},
		}, nil)
		require.Equal(t, evolution.OutcomeFail, o.Status)
		require.NotNil(t, o.Score)
		require.Contains(t, o.Notes, "economic_snapshot.json not found")
	})

	t.Run("notes are truncated to keep reviewer prompt small", func(t *testing.T) {
		long := strings.Repeat("x", 1000)
		o := outcomeFromEval(errors.New(long), nil, nil)
		require.LessOrEqual(t, len(o.Notes), 600)
		require.Contains(t, o.Notes, "agent error: ")
	})
}

func TestSeedManagedSkillsCopiesFolderTreeAndSkipsTopLevelFiles(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Two skill folders with an extra top-level file that must be ignored
	// (the on-disk layout is one folder per skill).
	skillA := filepath.Join(src, "weather-monitor")
	require.NoError(t, os.MkdirAll(filepath.Join(skillA, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillA, "SKILL.md"),
		[]byte("---\nname: Weather Monitor\n---\nbody"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(skillA, "docs", "notes.md"),
		[]byte("nested doc"), 0o644))

	skillB := filepath.Join(src, "recipe-cookbook")
	require.NoError(t, os.MkdirAll(skillB, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillB, "SKILL.md"),
		[]byte("---\nname: Recipe Cookbook\n---\nbody"), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(src, "stray.txt"),
		[]byte("ignored top-level file"), 0o644))

	n, err := seedManagedSkills(src, dst)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	require.FileExists(t, filepath.Join(dst, "weather-monitor", "SKILL.md"))
	require.FileExists(t, filepath.Join(dst, "weather-monitor", "docs", "notes.md"))
	require.FileExists(t, filepath.Join(dst, "recipe-cookbook", "SKILL.md"))
	_, err = os.Stat(filepath.Join(dst, "stray.txt"))
	require.True(t, os.IsNotExist(err), "top-level files must not be seeded")
}

func TestSeedManagedSkillsOverwritesExistingSkillFolder(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(src, "weather-monitor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "weather-monitor", "SKILL.md"),
		[]byte("new body"), 0o644))

	// Pre-populate dst with stale content that should be replaced.
	require.NoError(t, os.MkdirAll(filepath.Join(dst, "weather-monitor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dst, "weather-monitor", "SKILL.md"),
		[]byte("old body"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dst, "weather-monitor", "stale.md"),
		[]byte("stale sibling"), 0o644))

	n, err := seedManagedSkills(src, dst)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	body, err := os.ReadFile(filepath.Join(dst, "weather-monitor", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "new body", string(body))

	_, err = os.Stat(filepath.Join(dst, "weather-monitor", "stale.md"))
	require.True(t, os.IsNotExist(err), "stale siblings must be removed before seeding")
}

func TestReportScalesOmittedForExplicitTasks(t *testing.T) {
	cfg := &benchmarkConfig{
		Scales:        []string{"e1", "e2", "e3"},
		ExplicitTasks: []string{"openmeteo-weather/e1"},
	}
	require.Nil(t, reportScales(cfg))

	cfg = &benchmarkConfig{
		Scales: []string{"e1", "e2", "e3"},
	}
	require.Equal(t, []string{"e1", "e2", "e3"}, reportScales(cfg))
}
