package domain

import (
	"testing"
	"time"
)

func TestRouteMatchesHighestPriorityAndLeastLoad(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 30, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	input := RouteInput{
		Conversation: Conversation{ChannelAccountID: "account-1"},
		Customer:     Customer{Channel: ChannelService, Tags: []string{"vip", "existing"}},
		Account:      ChannelAccount{ID: "account-1", Channel: ChannelService},
		Rules: []RoutingRule{
			{ID: "general", TeamID: "team-general", Priority: 1, Active: true},
			{ID: "vip", TeamID: "team-vip", Priority: 10, Channel: ChannelService, AccountID: "account-1", Tag: "vip", StartMinute: 9 * 60, EndMinute: 18 * 60, Active: true},
			{ID: "inactive", TeamID: "team-wrong", Priority: 100, Channel: ChannelService},
		},
		Agents: []Agent{
			{ID: "agent-a", Active: true},
			{ID: "agent-b", Active: true},
			{ID: "agent-c", Active: true},
			{ID: "agent-inactive"},
			{ID: "agent-off-shift", Active: true},
			{ID: "agent-other-team", Active: true},
		},
		Shifts: []Shift{
			{ID: "shift-a", AgentID: "agent-a", TeamID: "team-vip", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour), State: "scheduled"},
			{ID: "shift-b", AgentID: "agent-b", TeamID: "team-vip", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour), State: "active"},
			{ID: "shift-c", AgentID: "agent-c", TeamID: "team-vip", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour), State: "on_duty"},
			{ID: "shift-inactive", AgentID: "agent-inactive", TeamID: "team-vip", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour)},
			{ID: "shift-off", AgentID: "agent-off-shift", TeamID: "team-vip", StartAt: now.Add(time.Hour), EndAt: now.Add(2 * time.Hour)},
			{ID: "shift-other", AgentID: "agent-other-team", TeamID: "team-general", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour)},
		},
		ActiveCounts: map[string]int{"agent-a": 4, "agent-b": 1, "agent-c": 2},
		Now:          now,
	}
	assignment, ok := Route(input)
	if !ok {
		t.Fatal("Route() did not assign an agent")
	}
	if assignment.RuleID != "vip" || assignment.TeamID != "team-vip" || assignment.AgentID != "agent-b" {
		t.Fatalf("Route() = %+v, want vip/agent-b", assignment)
	}
}

func TestRouteFiltersNonMatchingRules(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	agents := []Agent{{ID: "fallback-agent", Active: true}}
	shifts := []Shift{{AgentID: "fallback-agent", TeamID: "fallback", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour)}}
	tests := []struct {
		name string
		rule RoutingRule
	}{
		{name: "channel", rule: RoutingRule{Channel: ChannelMiniApp}},
		{name: "account", rule: RoutingRule{AccountID: "other"}},
		{name: "tag", rule: RoutingRule{Tag: "missing"}},
		{name: "before time", rule: RoutingRule{StartMinute: 11 * 60, EndMinute: 12 * 60}},
		{name: "at end", rule: RoutingRule{StartMinute: 9 * 60, EndMinute: 10 * 60}},
		{name: "invalid time", rule: RoutingRule{StartMinute: -1, EndMinute: 10 * 60}},
		{name: "empty equal time", rule: RoutingRule{StartMinute: 10 * 60, EndMinute: 10 * 60}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.rule.ID = "non-match"
			test.rule.TeamID = "unavailable"
			test.rule.Priority = 10
			test.rule.Active = true
			input := RouteInput{
				Conversation: Conversation{ChannelAccountID: "account"},
				Customer:     Customer{Channel: ChannelService, Tags: []string{"vip"}},
				Rules:        []RoutingRule{test.rule, {ID: "fallback", TeamID: "fallback", Active: true}},
				Agents:       agents, Shifts: shifts, Now: now,
			}
			assignment, ok := Route(input)
			if !ok || assignment.RuleID != "fallback" {
				t.Fatalf("Route() = %+v, %v; want fallback", assignment, ok)
			}
		})
	}
}

func TestRouteOvernightWindowAndBoundaries(t *testing.T) {
	agents := []Agent{{ID: "agent", Active: true}}
	for _, test := range []struct {
		name string
		hour int
		want bool
	}{
		{name: "before midnight", hour: 23, want: true},
		{name: "after midnight", hour: 1, want: true},
		{name: "outside", hour: 12},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 11, 13, test.hour, 0, 0, 0, time.UTC)
			input := RouteInput{
				Rules:  []RoutingRule{{ID: "overnight", TeamID: "team", StartMinute: 22 * 60, EndMinute: 2 * 60, Active: true}},
				Agents: agents,
				Shifts: []Shift{{AgentID: "agent", TeamID: "team", StartAt: now.Add(-time.Minute), EndAt: now.Add(time.Minute)}},
				Now:    now,
			}
			_, got := Route(input)
			if got != test.want {
				t.Fatalf("Route() assigned = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRouteRequiresMatchingActiveShift(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	base := RouteInput{
		Rules:  []RoutingRule{{ID: "rule", TeamID: "team", Active: true}},
		Agents: []Agent{{ID: "agent", Active: true}},
		Now:    now,
	}
	if _, ok := Route(base); ok {
		t.Fatal("Route() assigned without a shift")
	}
	base.Shifts = []Shift{{AgentID: "agent", TeamID: "team", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour), State: "off"}}
	if _, ok := Route(base); ok {
		t.Fatal("Route() assigned for an off-duty shift")
	}
	base.Shifts[0].State = ""
	base.Shifts[0].StartAt = now
	if _, ok := Route(base); !ok {
		t.Fatal("Route() did not assign at shift start")
	}
	base.Shifts[0].EndAt = now
	if _, ok := Route(base); ok {
		t.Fatal("Route() assigned at shift end")
	}
	base.Shifts[0].EndAt = now.Add(time.Hour)
	base.Shifts[0].TeamID = "other"
	if _, ok := Route(base); ok {
		t.Fatal("Route() assigned using another team's shift")
	}
}

func TestRouteTieBreaksByAgentIDAndDoesNotMutateRules(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	rules := []RoutingRule{
		{ID: "low", Priority: 1, TeamID: "wrong", Active: true},
		{ID: "high", Priority: 2, TeamID: "team", Active: true},
	}
	input := RouteInput{
		Rules:  rules,
		Agents: []Agent{{ID: "agent-b", Active: true}, {ID: "agent-a", Active: true}},
		Shifts: []Shift{
			{AgentID: "agent-a", TeamID: "team", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour)},
			{AgentID: "agent-b", TeamID: "team", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour)},
		},
		Now: now,
	}
	assignment, ok := Route(input)
	if !ok || assignment.AgentID != "agent-a" {
		t.Fatalf("Route() = %+v, %v; want agent-a", assignment, ok)
	}
	if rules[0].ID != "low" {
		t.Fatal("Route() mutated the caller's rules")
	}
}
