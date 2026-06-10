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
	"encoding/json"
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/evaluation/evalset"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/evaluator/llm/verifierpairwise"
	evaluatorregistry "trpc.group/trpc-go/trpc-agent-go/evaluation/evaluator/registry"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/metric"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

type gaiaPairwiseMessagesConstructor struct{}

func newGAIAVerifierRegistry() evaluatorregistry.Registry {
	r := evaluatorregistry.New()
	e := verifierpairwise.New(
		verifierpairwise.WithMessagesConstructor(newGAIAPairwiseMessagesConstructor()),
	)
	_ = r.Register(e.Name(), e)
	return r
}

func newGAIAPairwiseMessagesConstructor() *gaiaPairwiseMessagesConstructor {
	return &gaiaPairwiseMessagesConstructor{}
}

func (c *gaiaPairwiseMessagesConstructor) ConstructMessages(
	_ context.Context,
	actuals []*evalset.Invocation,
	expecteds []*evalset.Invocation,
	evalMetric *metric.EvalMetric,
) ([]model.Message, error) {
	if len(actuals) == 0 {
		return nil, fmt.Errorf("actuals is empty")
	}
	if len(expecteds) == 0 {
		return nil, fmt.Errorf("expecteds is empty")
	}
	actual := actuals[len(actuals)-1]
	expected := expecteds[len(expecteds)-1]
	if actual == nil {
		return nil, fmt.Errorf("actual invocation is nil")
	}
	if expected == nil {
		return nil, fmt.Errorf("expected invocation is nil")
	}
	userInput := messageText(actual.UserContent)
	if userInput == "" {
		userInput = messageText(expected.UserContent)
	}
	prompt := gaiaPairwisePrompt(
		userInput,
		rubricsText(evalMetric),
		formatCandidateForJudge(actual),
		formatCandidateForJudge(expected),
	)
	return []model.Message{model.NewUserMessage(prompt)}, nil
}

func gaiaPairwisePrompt(userInput string, criteria string, candidateA string, candidateB string) string {
	var b strings.Builder
	b.WriteString("# Mission\n\n")
	b.WriteString("You are an expert evaluator for GAIA benchmark agent answers. You will judge two candidate runs for the same user request.\n\n")
	b.WriteString("# Critical Rules\n\n")
	b.WriteString("- Judge only from the user request and each candidate's final answer plus evidence trace.\n")
	b.WriteString("- Do not reward confidence, fluent prose, or claims of verification unless the evidence trace supports the final answer.\n")
	b.WriteString("- The final answer must satisfy the exact wording, unit, scale, rounding, and formatting requested by the user.\n")
	b.WriteString("- If a candidate computes an intermediate value in a different unit or scale, judge whether the final response converts it back to the form requested by the user.\n")
	b.WriteString("- For computational tasks, inspect the code and tool output for modeling errors. A candidate with unsupported or incorrect code should score lower even if the final explanation sounds plausible.\n")
	b.WriteString("- For retrieval or extraction tasks, prefer answers whose evidence trace contains relevant retrieved content. Penalize answers that ignore requested sources or constraints.\n")
	b.WriteString("- Never use any hidden reference answer or benchmark ground truth. Only compare candidate quality from the visible request and candidate traces.\n\n")
	b.WriteString("# Mandatory Evaluation Steps\n\n")
	b.WriteString("1. First identify the required final answer form from the user request: the quantity being asked for, the unit or scale, the rounding rule, and any formatting constraints.\n")
	b.WriteString("2. Identify all explicit constraints in the request, such as source restrictions, date ranges, positional rules, ordering rules, separator style, and requested number of items.\n")
	b.WriteString("3. Extract each candidate's final answer and identify the value or values most directly supported by that candidate's visible evidence trace.\n")
	b.WriteString("4. Compare each final answer against both the required answer form and the value supported by its own evidence before considering fluency or evidence volume.\n")
	b.WriteString("5. If the user asks for a transformed or scaled quantity, the winning candidate must express the final answer in that requested transformed or scaled form.\n\n")
	b.WriteString("# Score Scale\n\n")
	b.WriteString("Use exactly one of 20 score tokens from A to T for each candidate. A is best and T is worst.\n")
	b.WriteString("- A = clearly and completely satisfies the request under the GAIA rules.\n")
	b.WriteString("- B-D = satisfies the request with only minor issues.\n")
	b.WriteString("- E-G = mostly correct with some issues.\n")
	b.WriteString("- H-J = uncertain, leans toward success.\n")
	b.WriteString("- K-M = uncertain, leans toward failure.\n")
	b.WriteString("- N-P = significant issues remain.\n")
	b.WriteString("- Q-S = failed with some partial progress.\n")
	b.WriteString("- T = clearly and completely fails.\n\n")
	if strings.TrimSpace(criteria) != "" {
		b.WriteString("# Additional Evaluation Criteria\n\n")
		b.WriteString(criteria)
		b.WriteString("\n\n")
	}
	b.WriteString("# Output Format\n\n")
	b.WriteString("First write a concise analysis that states the required final answer form and then compares the candidates. Then output the final score tags exactly once:\n\n")
	b.WriteString("<score_A>LETTER_A_TO_T</score_A>\n")
	b.WriteString("<score_B>LETTER_A_TO_T</score_B>\n\n")
	b.WriteString("# User Request\n\n")
	b.WriteString(userInput)
	b.WriteString("\n\n# Candidate A\n\n")
	b.WriteString(candidateA)
	b.WriteString("\n\n# Candidate B\n\n")
	b.WriteString(candidateB)
	return b.String()
}

func formatCandidateForJudge(inv *evalset.Invocation) string {
	var b strings.Builder
	b.WriteString("## Final Response\n\n")
	final := messageText(inv.FinalResponse)
	if final == "" {
		final = "<empty>"
	}
	b.WriteString(final)
	b.WriteString("\n\n## Evidence Trace\n\n")
	trace := candidateTrace(inv)
	if trace == "" {
		trace = "<no intermediate messages or tools recorded>"
	}
	b.WriteString(trace)
	return b.String()
}

func candidateTrace(inv *evalset.Invocation) string {
	if inv == nil {
		return ""
	}
	var b strings.Builder
	for i, msg := range inv.IntermediateResponses {
		text := messageText(msg)
		if text == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("### Intermediate Message %d\n", i+1))
		b.WriteString("Role: ")
		b.WriteString(string(msg.Role))
		b.WriteString("\nContent:\n")
		b.WriteString(text)
		b.WriteString("\n\n")
	}
	for i, tool := range inv.Tools {
		if tool == nil {
			continue
		}
		b.WriteString(fmt.Sprintf("### Tool Call %d\n", i+1))
		b.WriteString("Name: ")
		b.WriteString(tool.Name)
		b.WriteString("\nArguments:\n")
		b.WriteString(formatJudgeValue(tool.Arguments))
		b.WriteString("\nResult:\n")
		b.WriteString(formatJudgeValue(tool.Result))
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

func rubricsText(evalMetric *metric.EvalMetric) string {
	if evalMetric == nil || evalMetric.Criterion == nil || evalMetric.Criterion.LLMJudge == nil {
		return ""
	}
	var parts []string
	for _, rubric := range evalMetric.Criterion.LLMJudge.Rubrics {
		if rubric == nil || rubric.Content == nil || strings.TrimSpace(rubric.Content.Text) == "" {
			continue
		}
		id := strings.TrimSpace(rubric.ID)
		text := strings.TrimSpace(rubric.Content.Text)
		if id == "" {
			parts = append(parts, "- "+text)
			continue
		}
		parts = append(parts, fmt.Sprintf("- %s: %s", id, text))
	}
	return strings.Join(parts, "\n")
}

func messageText(msg *model.Message) string {
	if msg == nil {
		return ""
	}
	if strings.TrimSpace(msg.Content) != "" {
		return strings.TrimSpace(msg.Content)
	}
	var parts []string
	for _, part := range msg.ContentParts {
		if part.Type != model.ContentTypeText || part.Text == nil {
			continue
		}
		if text := strings.TrimSpace(*part.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func formatJudgeValue(value any) string {
	if value == nil {
		return "<nil>"
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}
