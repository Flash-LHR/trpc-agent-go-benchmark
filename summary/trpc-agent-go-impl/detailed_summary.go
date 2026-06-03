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
	"regexp"
	"strings"

	sessionsummary "trpc.group/trpc-go/trpc-agent-go/session/summary"
)

const detailedContinuityPrompt = "CRITICAL: Respond with TEXT ONLY. Do not call tools.\n\n" +
	"Your task is to create a detailed summary of the conversation so far, " +
	"paying close attention to the user's explicit requests and the " +
	"assistant's previous actions. The summary should preserve technical " +
	"details, code patterns, architectural decisions, errors, fixes, and " +
	"the exact next step needed to continue the work.\n\n" +
	"Before providing the final summary, write an <analysis> block to " +
	"chronologically review the conversation and double-check accuracy. " +
	"Then write a <summary> block with exactly these sections:\n\n" +
	"1. Primary Request and Intent\n" +
	"2. Key Technical Concepts\n" +
	"3. Files and Code Sections\n" +
	"4. Errors and fixes\n" +
	"5. Problem Solving\n" +
	"6. All user messages\n" +
	"7. Pending Tasks\n" +
	"8. Current Work\n" +
	"9. Optional Next Step\n\n" +
	"For section 6, list every user message that is not a tool result, in " +
	"order, preserving original wording. Do not paraphrase or omit any " +
	"user message; this section is intentionally exempt from any word " +
	"limit so it can fully preserve user intent.\n\n" +
	"For section 9, include the next step only when it directly follows " +
	"from the most recent explicit user request. Include direct quotes " +
	"from the recent conversation when they help avoid task drift.\n\n" +
	"<conversation>\n{conversation_text}\n</conversation>\n\n" +
	"Summary:"

var (
	detailedAnalysisBlockRE = regexp.MustCompile(`(?is)\**\s*<\s*analysis\s*>.*?<\s*/\s*analysis\s*>\s*\**`)
	detailedAnalysisOpenRE  = regexp.MustCompile(`(?i)\**\s*<\s*analysis\s*>\s*\**`)
	detailedAnalysisCloseRE = regexp.MustCompile(`(?i)\**\s*<\s*/\s*analysis\s*>\s*\**`)
	detailedSummaryOpenRE   = regexp.MustCompile(`(?i)\**\s*<\s*summary\s*>\s*\**`)
	detailedSummaryCloseRE  = regexp.MustCompile(`(?i)\**\s*<\s*/\s*summary\s*>\s*\**`)
)

func stripDetailedSummaryOutputHook(in *sessionsummary.PostSummaryHookContext) error {
	if in == nil {
		return nil
	}
	in.Summary = stripDetailedSummaryOutput(in.Summary)
	return nil
}

func stripDetailedSummaryOutput(text string) string {
	if loc := detailedSummaryOpenRE.FindStringIndex(text); loc != nil {
		inner := text[loc[1]:]
		if cl := detailedSummaryCloseRE.FindStringIndex(inner); cl != nil {
			inner = inner[:cl[0]]
		}
		return strings.TrimSpace(inner)
	}
	text = detailedAnalysisBlockRE.ReplaceAllString(text, "")
	text = detailedAnalysisOpenRE.ReplaceAllString(text, "")
	text = detailedAnalysisCloseRE.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}
