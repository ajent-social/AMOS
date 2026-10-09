package host

import (
	"context"
	"encoding/base64"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ajent-social/amos/examples/continuity/domain"
	"github.com/ajent-social/amos/examples/continuity/guide"
	"github.com/google/uuid"
)

const baselineTestID = "01900000-0000-7000-8000-000000000001"

func baselineTestToken() string { return base64.RawURLEncoding.EncodeToString(make([]byte, 32)) }

func TestBaselineReadSelectors(t *testing.T) {
	valid := []baselineQuery{
		{Page: pageProperties, Limit: 25, Query: " %_ "},
		{Page: pageProperty, ID: baselineTestID},
		{Page: pageCases, Limit: 1, After: baselineTestID},
		{Page: pageCase, ID: baselineTestID, CSRFToken: baselineTestToken()},
		{Page: pageApplications, Limit: 100},
		{Page: pageApplication, ID: baselineTestID, CSRFToken: baselineTestToken()},
		{Page: pageProcedures, Limit: 25},
		{Page: pageProcedure, ID: baselineTestID, CSRFToken: baselineTestToken()},
		{Page: pageActivity, Limit: 25},
		{Page: pageGuide, Topic: guide.Attention, Limit: 25},
		{Page: pageGuide, Topic: guide.Handover, Limit: 100},
		{Page: pageGuide, Topic: guide.ExplainCase, CaseID: baselineTestID},
		{Page: pageGuide, Topic: guide.OwnerDraft, CaseID: baselineTestID},
		{Page: pageGuide, Topic: guide.SpendingAuthority},
	}
	for i, q := range valid {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			q.WorkspaceID = uuid.MustParse(baselineTestID)
			q.Fragment = true
			got, ok := normalizeBaselineQuery(q)
			if !ok {
				t.Fatal("declared selector rejected")
			}
			want := q
			want.Query = strings.TrimSpace(want.Query)
			if got != want {
				t.Fatal("selector mutated beyond query normalization")
			}
		})
	}
	for _, value := range []string{"", "  ", strings.Repeat("界", 120)} {
		if _, ok := normalizeBaselineQuery(baselineQuery{Page: pageProperties, Limit: 1, Query: value}); !ok {
			t.Fatal("valid normalized property query rejected")
		}
	}
}

func TestBaselineReadRejectsCombinations(t *testing.T) {
	tests := map[string]baselineQuery{
		"unknown": {Page: 255}, "empty": {},
		"workspace":     {Page: pageProperties, Limit: 1, WorkspaceID: uuid.New()},
		"long query":    {Page: pageProperties, Limit: 1, Query: strings.Repeat("a", 481)},
		"rune query":    {Page: pageProperties, Limit: 1, Query: strings.Repeat("界", 121)},
		"query control": {Page: pageProperties, Limit: 1, Query: "x\n"},
		"query utf8":    {Page: pageProperties, Limit: 1, Query: "\xff"},
		"list id":       {Page: pageProperties, Limit: 1, ID: baselineTestID},
		"list topic":    {Page: pageProperties, Limit: 1, Topic: guide.Attention},
		"list case":     {Page: pageProperties, Limit: 1, CaseID: baselineTestID},
		"list token":    {Page: pageProperties, Limit: 1, CSRFToken: baselineTestToken()},
		"zero limit":    {Page: pageProperties}, "high limit": {Page: pageCases, Limit: 101},
		"bad after":              {Page: pageCases, Limit: 1, After: "bad"},
		"unsupported query":      {Page: pageCases, Limit: 1, Query: " "},
		"detail missing id":      {Page: pageProperty},
		"detail bad id":          {Page: pageProperty, ID: uuid.New().String()},
		"detail after":           {Page: pageProperty, ID: baselineTestID, After: baselineTestID},
		"detail query":           {Page: pageProperty, ID: baselineTestID, Query: " "},
		"detail limit":           {Page: pageProperty, ID: baselineTestID, Limit: 1},
		"detail topic":           {Page: pageProperty, ID: baselineTestID, Topic: guide.Attention},
		"detail case":            {Page: pageProperty, ID: baselineTestID, CaseID: baselineTestID},
		"property token":         {Page: pageProperty, ID: baselineTestID, CSRFToken: baselineTestToken()},
		"case missing token":     {Page: pageCase, ID: baselineTestID},
		"application bad token":  {Page: pageApplication, ID: baselineTestID, CSRFToken: strings.Repeat("!", 43)},
		"procedure padded token": {Page: pageProcedure, ID: baselineTestID, CSRFToken: baselineTestToken() + "="},
		"guide id":               {Page: pageGuide, Topic: guide.Attention, Limit: 1, ID: baselineTestID},
		"guide after":            {Page: pageGuide, Topic: guide.Attention, Limit: 1, After: baselineTestID},
		"guide query":            {Page: pageGuide, Topic: guide.Attention, Limit: 1, Query: " "},
		"guide token":            {Page: pageGuide, Topic: guide.Attention, Limit: 1, CSRFToken: baselineTestToken()},
		"guide unknown":          {Page: pageGuide, Topic: "unknown"},
		"guide missing case":     {Page: pageGuide, Topic: guide.ExplainCase},
		"guide case limit":       {Page: pageGuide, Topic: guide.OwnerDraft, CaseID: baselineTestID, Limit: 1},
		"guide attention case":   {Page: pageGuide, Topic: guide.Attention, Limit: 1, CaseID: baselineTestID},
		"guide attention limit":  {Page: pageGuide, Topic: guide.Attention},
		"guide spending case":    {Page: pageGuide, Topic: guide.SpendingAuthority, CaseID: baselineTestID},
		"guide spending limit":   {Page: pageGuide, Topic: guide.SpendingAuthority, Limit: 1},
		"oversized id":           {Page: pageProperty, ID: strings.Repeat("x", 37)},
		"oversized topic":        {Page: pageGuide, Topic: guide.Topic(strings.Repeat("x", 19))},
	}
	for name, q := range tests {
		t.Run(name, func(t *testing.T) {
			if got, ok := normalizeBaselineQuery(q); ok || got != (baselineQuery{}) {
				t.Fatal("invalid selector admitted or not zero")
			}
		})
	}
}

func TestBaselineGuideReferences(t *testing.T) {
	other := "01900000-0000-7000-8000-000000000002"
	cases := []domain.Case{{SourceIDs: []string{other, baselineTestID}}, {SourceIDs: []string{baselineTestID}}}
	got, e := baselineGuideReferences(cases)
	if e != nil || !reflect.DeepEqual(got, []string{baselineTestID, other}) {
		t.Fatal("references not deduplicated and sorted")
	}
	got[0] = other
	if cases[0].SourceIDs[1] != baselineTestID {
		t.Fatal("input alias retained")
	}
	var hundred []domain.Case
	for i := 1; i <= 100; i++ {
		hundred = append(hundred, domain.Case{SourceIDs: []string{fmt.Sprintf("01900000-0000-7000-8000-%012x", i)}})
	}
	if ids, e := baselineGuideReferences(hundred); e != nil || len(ids) != 100 {
		t.Fatal("maximum distinct references rejected")
	}
	hundred[0].SourceIDs = append(hundred[0].SourceIDs, "01900000-0000-7000-8000-000000000101")
	if ids, e := baselineGuideReferences(hundred); ids != nil || e != errUnavailable {
		t.Fatal("oversized guide silently truncated or admitted")
	}
	for _, bad := range [][]domain.Case{{{SourceIDs: nil}}, {{SourceIDs: []string{"bad"}}}, {{SourceIDs: make([]string, 33)}}, make([]domain.Case, 101)} {
		if ids, e := baselineGuideReferences(bad); ids != nil || e != errUnavailable {
			t.Fatal("bad case references admitted")
		}
	}
}

func TestBaselineReadUnavailableWithoutCore(t *testing.T) {
	var c *readCore
	if out, e := c.baseline(context.Background(), baselineQuery{Page: pageProperties, Limit: 1}); out != nil || e != errUnavailable {
		t.Fatal("missing private core admitted")
	}
}
