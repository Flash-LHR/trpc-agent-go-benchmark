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
	"testing"

	"trpc.group/trpc-go/trpc-agent-go-benchmark/summary/trpc-agent-go-impl/evaluation/dataset"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestRoundRobinQMSumCasesByMeeting(t *testing.T) {
	t.Parallel()

	cases := []*dataset.QMSumCase{
		{CaseID: "b_02", MeetingID: "b"},
		{CaseID: "a_01", MeetingID: "a"},
		{CaseID: "a_02", MeetingID: "a"},
		{CaseID: "b_01", MeetingID: "b"},
	}

	got := roundRobinQMSumCasesByMeeting(cases)
	want := []string{"a_01", "b_01", "a_02", "b_02"}
	if len(got) != len(want) {
		t.Fatalf("len(roundRobinQMSumCasesByMeeting) = %d, want %d", len(got), len(want))
	}
	for i, qcase := range got {
		if qcase.CaseID != want[i] {
			t.Fatalf("case[%d] = %s, want %s", i, qcase.CaseID, want[i])
		}
	}
}

func TestFilterQMSumCasesBySupportDistance(t *testing.T) {
	t.Parallel()

	cases := []*dataset.QMSumCase{
		{
			CaseID:           "keep",
			RelevantTextSpan: [][]string{{"1", "2"}},
			Transcript:       make([]dataset.QMSumTranscriptTurn, 12),
		},
		{
			CaseID:           "drop",
			RelevantTextSpan: [][]string{{"8", "9"}},
			Transcript:       make([]dataset.QMSumTranscriptTurn, 12),
		},
		{
			CaseID:     "no_support",
			Transcript: make([]dataset.QMSumTranscriptTurn, 12),
		},
	}

	got := filterQMSumCasesBySupportDistance(cases, 5)
	if len(got) != 1 || got[0].CaseID != "keep" {
		t.Fatalf("filterQMSumCasesBySupportDistance = %+v, want only keep", got)
	}
}

func TestConsumeEventsWithToolStats(t *testing.T) {
	t.Parallel()

	evtCh := make(chan *event.Event, 2)
	evtCh <- event.New("1", "test", event.WithResponse(&model.Response{
		Done: true,
		Choices: []model.Choice{{
			Message: model.Message{
				ToolCalls: []model.ToolCall{
					{
						ID:   "call-search",
						Type: "function",
						Function: model.FunctionDefinitionParam{
							Name: "session_search",
						},
					},
					{
						ID:   "call-load",
						Type: "function",
						Function: model.FunctionDefinitionParam{
							Name: "session_load",
						},
					},
				},
			},
		}},
	}))
	evtCh <- event.New("2", "test", event.WithResponse(&model.Response{
		Done: true,
		Choices: []model.Choice{{
			Message: model.Message{
				Content: "final answer",
			},
		}},
		Usage: &model.Usage{
			PromptTokens:     11,
			CompletionTokens: 7,
			TotalTokens:      18,
		},
	}))
	close(evtCh)

	answer, usage, toolStats := consumeEventsWithToolStats(evtCh)
	if answer != "final answer" {
		t.Fatalf("answer = %q, want final answer", answer)
	}
	if usage.TotalTokens != 18 {
		t.Fatalf("usage.TotalTokens = %d, want 18", usage.TotalTokens)
	}
	if toolStats.Count("session_search") != 1 {
		t.Fatalf("session_search count = %d, want 1", toolStats.Count("session_search"))
	}
	if toolStats.Count("session_load") != 1 {
		t.Fatalf("session_load count = %d, want 1", toolStats.Count("session_load"))
	}
}
