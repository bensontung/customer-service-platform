package domain

import (
	"sort"
	"strings"
	"time"
)

type RouteInput struct {
	Conversation Conversation
	Customer     Customer
	Account      ChannelAccount
	Rules        []RoutingRule
	Agents       []Agent
	Shifts       []Shift
	ActiveCounts map[string]int
	Now          time.Time
}

type Assignment struct {
	RuleID  string
	TeamID  string
	AgentID string
}

func Route(input RouteInput) (Assignment, bool) {
	now := input.Now
	rules := append([]RoutingRule(nil), input.Rules...)
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].Priority == rules[j].Priority {
			return rules[i].ID < rules[j].ID
		}
		return rules[i].Priority > rules[j].Priority
	})
	for _, rule := range rules {
		if !matchesRule(rule, input, now) {
			continue
		}
		agentID, ok := leastLoadedAgent(rule.TeamID, input.Agents, input.Shifts, input.ActiveCounts, now)
		if ok {
			return Assignment{RuleID: rule.ID, TeamID: rule.TeamID, AgentID: agentID}, true
		}
	}
	return Assignment{}, false
}

func matchesRule(rule RoutingRule, input RouteInput, now time.Time) bool {
	if !rule.Active || rule.TeamID == "" {
		return false
	}
	if rule.Channel != "" && rule.Channel != input.conversationChannel() ||
		rule.AccountID != "" && rule.AccountID != input.Conversation.ChannelAccountID && rule.AccountID != input.Account.ID ||
		rule.Tag != "" && !hasTag(input.Customer.Tags, rule.Tag) ||
		!withinMinutes(rule.StartMinute, rule.EndMinute, now.Hour()*60+now.Minute()) {
		return false
	}
	return true
}

func (input RouteInput) conversationChannel() string {
	if input.Account.Channel != "" {
		return input.Account.Channel
	}
	return input.Customer.Channel
}

func withinMinutes(start, end, current int) bool {
	if start == 0 && end == 0 {
		return true
	}
	if start < 0 || start >= 1440 || end < 0 || end >= 1440 || start == end {
		return false
	}
	if start < end {
		return current >= start && current < end
	}
	return current >= start || current < end
}

func hasTag(tags []string, wanted string) bool {
	for _, tag := range tags {
		if tag == wanted {
			return true
		}
	}
	return false
}

func onShift(agentID, teamID string, shifts []Shift, now time.Time) bool {
	for _, shift := range shifts {
		if shift.AgentID != agentID || shift.TeamID != "" && shift.TeamID != teamID ||
			now.Before(shift.StartAt.UTC()) || !now.Before(shift.EndAt.UTC()) {
			continue
		}
		switch strings.ToLower(shift.State) {
		case "", "scheduled", "active", "on", "on_duty", "working":
			return true
		}
	}
	return false
}

func leastLoadedAgent(teamID string, agents []Agent, shifts []Shift, activeCounts map[string]int, now time.Time) (string, bool) {
	candidates := make([]Agent, 0)
	for _, agent := range agents {
		if agent.Active && onShift(agent.ID, teamID, shifts, now) {
			candidates = append(candidates, agent)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	if len(candidates) == 0 {
		return "", false
	}
	selected := candidates[0]
	for _, candidate := range candidates[1:] {
		if activeCounts[candidate.ID] < activeCounts[selected.ID] {
			selected = candidate
		}
	}
	return selected.ID, true
}
