package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/zeromicro/go-zero/rest/token"
)

func TestAgentIDSupportsGoZeroJWTContext(t *testing.T) {
	agent := domain.Agent{ID: "agent-1", Role: domain.RoleAgent, Active: true}
	secret := "integration-test-secret-at-least-32-bytes"
	session, err := Issue(secret, agent, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	parser := token.NewTokenParser()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer "+session)
	request = request.WithContext(context.WithValue(request.Context(), "agentId", "agent-1")) //nolint:staticcheck // Simulates go-zero's JWT claim context conversion.
	if _, err = parser.ParseToken(request, secret, ""); err != nil {
		t.Fatal(err)
	}
	identifier, err := AgentID(request.Context())
	if err != nil || identifier != agent.ID {
		t.Fatalf("AgentID() = %q, %v", identifier, err)
	}
	if _, err = AgentID(context.Background()); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("empty AgentID() error = %v", err)
	}
}
