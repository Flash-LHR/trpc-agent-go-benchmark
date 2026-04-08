//
// Tencent is pleased to support the open source community by making
// trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package dataset

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// QMSumMeeting represents a single QMSum meeting JSON.
type QMSumMeeting struct {
	MeetingID string `json:"-"`
	Domain    string `json:"-"`

	TopicList         []QMSumTopic          `json:"topic_list"`
	GeneralQueryList  []QMSumQuery          `json:"general_query_list"`
	SpecificQueryList []QMSumQuery          `json:"specific_query_list"`
	MeetingTurns      []QMSumTranscriptTurn `json:"meeting_transcripts"`
}

// QMSumTopic represents one topic block in QMSum.
type QMSumTopic struct {
	Topic            string     `json:"topic"`
	RelevantTextSpan [][]string `json:"relevant_text_span"`
}

// QMSumQuery represents one QMSum query-answer pair.
type QMSumQuery struct {
	Query            string     `json:"query"`
	Answer           string     `json:"answer"`
	RelevantTextSpan [][]string `json:"relevant_text_span"`
}

// QMSumTranscriptTurn represents one meeting transcript turn.
type QMSumTranscriptTurn struct {
	Speaker string `json:"speaker"`
	Content string `json:"content"`
}

// QMSumCase is one flattened evaluation case derived from a meeting query.
type QMSumCase struct {
	CaseID           string
	MeetingID        string
	Domain           string
	QueryType        string
	Query            string
	Answer           string
	RelevantTextSpan [][]string
	Transcript       []QMSumTranscriptTurn
}

// LoadQMSum loads flattened QMSum cases from <dataDir>/data/<domain>/<split>.
// Supported domains: ALL, Academic, Committee, Product.
// Supported query types: specific, general, all.
func (l *DatasetLoader) LoadQMSum(
	split, domain, queryType string,
) ([]*QMSumCase, error) {
	split = normalizeQMSumSplit(split)
	domainDir, err := normalizeQMSumDomain(domain)
	if err != nil {
		return nil, err
	}
	queryType, err = normalizeQMSumQueryType(queryType)
	if err != nil {
		return nil, err
	}

	root := filepath.Join(l.dataDir, "data", domainDir, split)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read QMSum dir %s: %w", root, err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		files = append(files, filepath.Join(root, entry.Name()))
	}
	sort.Strings(files)

	cases := make([]*QMSumCase, 0, len(files)*2)
	for _, path := range files {
		meeting, err := loadQMSumMeeting(path, domainDir)
		if err != nil {
			return nil, err
		}
		cases = append(cases, flattenQMSumMeeting(meeting, queryType)...)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf(
			"no QMSum cases found under domain=%s split=%s query_type=%s",
			domainDir, split, queryType,
		)
	}
	return cases, nil
}

func loadQMSumMeeting(path, domain string) (*QMSumMeeting, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read QMSum file %s: %w", path, err)
	}

	var meeting QMSumMeeting
	if err := json.Unmarshal(data, &meeting); err != nil {
		return nil, fmt.Errorf("parse QMSum file %s: %w", path, err)
	}

	meeting.Domain = domain
	meeting.MeetingID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return &meeting, nil
}

func flattenQMSumMeeting(
	meeting *QMSumMeeting,
	queryType string,
) []*QMSumCase {
	if meeting == nil {
		return nil
	}

	cases := make([]*QMSumCase, 0, len(meeting.GeneralQueryList)+len(meeting.SpecificQueryList))
	if queryType == "general" || queryType == "all" {
		for i, query := range meeting.GeneralQueryList {
			cases = append(cases, &QMSumCase{
				CaseID:           fmt.Sprintf("%s_general_%02d", meeting.MeetingID, i+1),
				MeetingID:        meeting.MeetingID,
				Domain:           meeting.Domain,
				QueryType:        "general",
				Query:            strings.TrimSpace(query.Query),
				Answer:           strings.TrimSpace(query.Answer),
				RelevantTextSpan: query.RelevantTextSpan,
				Transcript:       cloneQMSumTranscript(meeting.MeetingTurns),
			})
		}
	}
	if queryType == "specific" || queryType == "all" {
		for i, query := range meeting.SpecificQueryList {
			cases = append(cases, &QMSumCase{
				CaseID:           fmt.Sprintf("%s_specific_%02d", meeting.MeetingID, i+1),
				MeetingID:        meeting.MeetingID,
				Domain:           meeting.Domain,
				QueryType:        "specific",
				Query:            strings.TrimSpace(query.Query),
				Answer:           strings.TrimSpace(query.Answer),
				RelevantTextSpan: query.RelevantTextSpan,
				Transcript:       cloneQMSumTranscript(meeting.MeetingTurns),
			})
		}
	}
	return cases
}

func cloneQMSumTranscript(src []QMSumTranscriptTurn) []QMSumTranscriptTurn {
	if len(src) == 0 {
		return nil
	}
	dst := make([]QMSumTranscriptTurn, len(src))
	copy(dst, src)
	return dst
}

func normalizeQMSumSplit(split string) string {
	switch strings.ToLower(strings.TrimSpace(split)) {
	case "train":
		return "train"
	case "val", "valid", "validation":
		return "val"
	case "test":
		fallthrough
	default:
		return "test"
	}
}

func normalizeQMSumDomain(domain string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "", "all":
		return "ALL", nil
	case "academic":
		return "Academic", nil
	case "committee":
		return "Committee", nil
	case "product":
		return "Product", nil
	default:
		return "", fmt.Errorf(
			"invalid QMSum domain %q, valid values: ALL, Academic, Committee, Product",
			domain,
		)
	}
}

func normalizeQMSumQueryType(queryType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(queryType)) {
	case "", "specific":
		return "specific", nil
	case "general":
		return "general", nil
	case "all":
		return "all", nil
	default:
		return "", fmt.Errorf(
			"invalid QMSum query type %q, valid values: specific, general, all",
			queryType,
		)
	}
}
