//
// Tencent is pleased to support the open source community by making
// trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package main

import "testing"

func TestStripDetailedSummaryOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "unwraps summary block",
			in:   "<analysis>scratch</analysis>\n<summary>kept</summary>",
			want: "kept",
		},
		{
			name: "handles markdown-wrapped unclosed markers",
			in:   "**<analysis>**\n\nscratch\n\n**<summary>**\n\n1. kept",
			want: "1. kept",
		},
		{
			name: "strips analysis without summary",
			in:   "<analysis>scratch</analysis>\nplain",
			want: "plain",
		},
		{
			name: "trims plain text",
			in:   "  plain  ",
			want: "plain",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := stripDetailedSummaryOutput(tt.in); got != tt.want {
				t.Fatalf("stripDetailedSummaryOutput() = %q, want %q", got, tt.want)
			}
		})
	}
}
