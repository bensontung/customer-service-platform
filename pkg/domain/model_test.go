package domain

import (
	"errors"
	"testing"
	"time"
)

func TestNewReplyWindow(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	received := time.Date(2026, 11, 13, 9, 30, 0, 0, location)
	deadline := NewReplyWindow(received)
	if deadline.Location() != time.UTC {
		t.Fatalf("deadline location = %v, want UTC", deadline.Location())
	}
	if got := deadline.Sub(received); got != 48*time.Hour {
		t.Fatalf("window = %v, want 48h", got)
	}
}

func TestCanReply(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	agent := Agent{ID: "agent-1", Role: RoleAgent, Active: true}
	conversation := Conversation{
		Status: StatusOpen, AssigneeID: agent.ID, ReplyDeadlineAt: now.Add(time.Hour),
	}
	if err := CanReply(conversation, agent, now); err != nil {
		t.Fatalf("CanReply() error = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*Conversation, *Agent)
		now    time.Time
		want   error
	}{
		{name: "closed", mutate: func(c *Conversation, _ *Agent) { c.Status = StatusClosed }, want: ErrInvalidRequest},
		{name: "unassigned", mutate: func(c *Conversation, _ *Agent) { c.AssigneeID = "" }, want: ErrNotAssigned},
		{name: "other agent", mutate: func(_ *Conversation, a *Agent) { a.ID = "agent-2" }, want: ErrNotAssigned},
		{name: "inactive", mutate: func(_ *Conversation, a *Agent) { a.Active = false }, want: ErrNotAssigned},
		{name: "at deadline", now: conversation.ReplyDeadlineAt, want: ErrReplyExpired},
		{name: "expired", now: conversation.ReplyDeadlineAt.Add(time.Nanosecond), want: ErrReplyExpired},
		{name: "five replies", mutate: func(c *Conversation, _ *Agent) { c.RepliesInWindow = 5 }, want: ErrReplyQuota},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, a := conversation, agent
			if test.mutate != nil {
				test.mutate(&c, &a)
			}
			at := now
			if !test.now.IsZero() {
				at = test.now
			}
			if err := CanReply(c, a, at); !errors.Is(err, test.want) {
				t.Fatalf("CanReply() error = %v, want %v", err, test.want)
			}
		})
	}
	supervisor := Agent{ID: "supervisor", Role: RoleSupervisor, Active: true}
	if err := CanReply(conversation, supervisor, now); err != nil {
		t.Fatalf("supervisor CanReply() error = %v", err)
	}
}

func TestCanAccess(t *testing.T) {
	conversation := Conversation{AssigneeID: "agent-1"}
	tests := []struct {
		name  string
		agent Agent
		want  bool
	}{
		{name: "owner", agent: Agent{ID: "agent-1", Role: RoleAgent, Active: true}, want: true},
		{name: "other", agent: Agent{ID: "agent-2", Role: RoleAgent, Active: true}},
		{name: "supervisor", agent: Agent{ID: "agent-2", Role: RoleSupervisor, Active: true}, want: true},
		{name: "admin", agent: Agent{ID: "agent-2", Role: RoleAdmin, Active: true}, want: true},
		{name: "inactive admin", agent: Agent{ID: "agent-2", Role: RoleAdmin}},
		{name: "unassigned queue", agent: Agent{ID: "agent-2", Role: RoleAgent, Active: true}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := conversation
			if test.name == "unassigned queue" {
				c = Conversation{}
			}
			if got := CanAccess(c, test.agent); got != test.want {
				t.Fatalf("CanAccess() = %v, want %v", got, test.want)
			}
		})
	}
}
