//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

package gaiaeval

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/evalset"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/metric"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/metric/criterion"
	criterionllm "trpc.group/trpc-go/trpc-agent-go/evaluation/metric/criterion/llm"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

func TestGAIAFinalAnswerVerifier(t *testing.T) {
	t.Parallel()

	v := gaiaFinalAnswerVerifier{}

	res, err := v.Verify(
		context.Background(),
		&agent.Invocation{},
		nil,
	)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.Passed {
		t.Fatalf("Passed = true, want false")
	}

	okEvt := &event.Event{
		Response: &model.Response{
			Done: true,
			Choices: []model.Choice{{
				Index: 0,
				Message: model.Message{
					Role:    model.RoleAssistant,
					Content: "FINAL ANSWER: 42",
				},
			}},
		},
	}
	res, err = v.Verify(context.Background(), &agent.Invocation{}, okEvt)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.Passed {
		t.Fatalf("Passed = false, want true")
	}

	partText := "FINAL ANSWER: 42"
	okEvtParts := &event.Event{
		Response: &model.Response{
			Done: true,
			Choices: []model.Choice{{
				Index: 0,
				Message: model.Message{
					Role: model.RoleAssistant,
					ContentParts: []model.ContentPart{{
						Type: model.ContentTypeText,
						Text: &partText,
					}},
				},
			}},
		},
	}
	res, err = v.Verify(context.Background(), &agent.Invocation{}, okEvtParts)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.Passed {
		t.Fatalf("Passed = false, want true (ContentParts)")
	}

	badEvt := &event.Event{
		Response: &model.Response{
			Done: true,
			Choices: []model.Choice{{
				Index: 0,
				Message: model.Message{
					Role:    model.RoleAssistant,
					Content: "Answer: 42",
				},
			}},
		},
	}
	res, err = v.Verify(context.Background(), &agent.Invocation{}, badEvt)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.Passed {
		t.Fatalf("Passed = true, want false")
	}
	if !strings.Contains(res.Feedback, gaiaFinalAnswerPrefix) {
		t.Fatalf(
			"feedback missing %q: %q",
			gaiaFinalAnswerPrefix,
			res.Feedback,
		)
	}
}

func TestGAIAFinalAnswerVerifier_ReturnTypes(t *testing.T) {
	t.Parallel()

	var v gaiaFinalAnswerVerifier
	var _ runner.Verifier = v
}

func TestExtractFinalAnswer_BlockFinalAnswer(t *testing.T) {
	t.Parallel()

	content := `/*REASONING*/
Simulation and analysis show position scores: pos1 = 1/3, pos2 = 5/9, pos3 = 17/27.

/*FINAL_ANSWER*/
3`

	assert.Equal(t, "3", extractFinalAnswer(content))
}

func TestExtractFinalAnswer_BlockFinalAnswerWithPrefix(t *testing.T) {
	t.Parallel()

	content := `/*REASONING*/
The candidate should return a concise final answer.

/*FINAL ANSWER*/
FINAL ANSWER: 100`

	assert.Equal(t, "100", extractFinalAnswer(content))
}

func TestVerifyAnswer_NumericDoesNotUseContains(t *testing.T) {
	t.Parallel()

	assert.False(t, verifyAnswer("17000", "17"))
	assert.True(t, verifyAnswer("17", "17"))
}

func TestFormatAnswer_PreservesNumericCommaList(t *testing.T) {
	t.Parallel()

	assert.Equal(
		t,
		"132, 133, 134, 197, 245",
		formatAnswer("132,133,134,197,245"),
	)
	assert.Equal(t, "89706", formatAnswer("89,706"))
}

func TestVerifyAnswer_NumericCommaList(t *testing.T) {
	t.Parallel()

	assert.True(
		t,
		verifyAnswer("132,133,134,197,245", "132, 133, 134, 197, 245"),
	)
	assert.True(t, verifyAnswer("89,706", "89706"))
}

func TestGAIAPairwiseMessagesConstructor_IncludesTraceWithoutGroundTruth(t *testing.T) {
	t.Parallel()

	constructor := newGAIAPairwiseMessagesConstructor()
	userContent := model.NewUserMessage("How many thousand hours are in 17000 hours?")
	actualFinal := model.NewAssistantMessage("FINAL ANSWER: 17000")
	expectedFinal := model.NewAssistantMessage("FINAL ANSWER: 17")
	actual := &evalset.Invocation{
		UserContent:   &userContent,
		FinalResponse: &actualFinal,
		Tools: []*evalset.Tool{{
			Name:      "execute_python",
			Arguments: map[string]any{"code": "print(17000 / 1000)"},
			Result:    "17",
		}},
	}
	expected := &evalset.Invocation{
		UserContent:   &userContent,
		FinalResponse: &expectedFinal,
	}
	messages, err := constructor.ConstructMessages(
		context.Background(),
		[]*evalset.Invocation{actual},
		[]*evalset.Invocation{expected},
		&metric.EvalMetric{
			Criterion: criterion.New(
				criterion.WithLLMJudge(
					criterionllm.New("", "",
						criterionllm.WithRubrics([]*criterionllm.Rubric{{
							ID: "unit-scale",
							Content: &criterionllm.RubricContent{
								Text: "Respect the requested unit scale.",
							},
						}}),
					),
				),
			),
		},
	)

	assert.NoError(t, err)
	assert.Len(t, messages, 1)
	prompt := messages[0].Content
	assert.Contains(t, prompt, "How many thousand hours are in 17000 hours?")
	assert.Contains(t, prompt, "FINAL ANSWER: 17000")
	assert.Contains(t, prompt, "FINAL ANSWER: 17")
	assert.Contains(t, prompt, "execute_python")
	assert.Contains(t, prompt, "print(17000 / 1000)")
	assert.Contains(t, prompt, "Never use any hidden reference answer")
	assert.Contains(t, prompt, "Respect the requested unit scale.")
	assert.Contains(t, prompt, "<score_A>LETTER_A_TO_T</score_A>")
}

func TestGAIAPairwisePrompt_DoesNotContainSpecificBenchmarkCase(t *testing.T) {
	t.Parallel()

	prompt := gaiaPairwisePrompt("", "", "", "")

	assert.NotContains(t, prompt, "17000")
	assert.NotContains(t, prompt, "thousand hours")
	assert.NotContains(t, prompt, "Kipchoge")
	assert.NotContains(t, prompt, "Moon")
	assert.Contains(t, prompt, "Mandatory Evaluation Steps")
	assert.Contains(t, prompt, "required final answer form")
	assert.Contains(t, prompt, "explicit constraints")
	assert.Contains(t, prompt, "visible evidence trace")
	assert.Contains(t, prompt, "unit or scale")
	assert.Contains(t, prompt, "form requested by the user")
}

func TestGAIALLMVerifierMetric_RubricsAreGeneric(t *testing.T) {
	t.Parallel()

	evalMetric := gaiaLLMVerifierMetric()
	var rubricText strings.Builder
	for _, rubric := range evalMetric.Criterion.LLMJudge.Rubrics {
		rubricText.WriteString(rubric.ID)
		rubricText.WriteString("\n")
		rubricText.WriteString(rubric.Content.Text)
		rubricText.WriteString("\n")
	}
	text := rubricText.String()

	assert.NotContains(t, text, "17000")
	assert.NotContains(t, text, "thousand hours")
	assert.NotContains(t, text, "Kipchoge")
	assert.NotContains(t, text, "Moon")
	assert.NotContains(t, text, "Judge only from the user request and the two final responses")
	assert.Contains(t, text, "answer_form_alignment")
	assert.Contains(t, text, "evidence_grounding")
	assert.Contains(t, text, "final_answer_fidelity")
	assert.Contains(t, text, "constraint_completion")
	assert.Contains(t, text, "concise_extractable_answer")
	assert.Contains(t, text, "bare value or include labels")
	assert.Contains(t, text, "visible tool outputs")
	assert.Contains(t, text, "preserves the exact value supported by its own evidence trace")
	assert.Contains(t, text, "positional selection rules")
}
