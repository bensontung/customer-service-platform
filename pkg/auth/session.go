package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const AgentIDKey contextKey = "agentId"

func Issue(secret string, agent domain.Agent, lifetime time.Duration) (string, error) {
	if len(secret) < 32 || lifetime <= 0 || lifetime > 30*time.Minute {
		return "", fmt.Errorf("session secret must be at least 32 bytes and lifetime must not exceed 30 minutes")
	}
	now := time.Now().UTC()
	claims := jwt.MapClaims{"agentId": agent.ID, "sub": agent.ID, "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(lifetime).Unix(), "jti": domain.NewID()}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func AgentID(ctx context.Context) (string, error) {
	id, ok := ctx.Value(AgentIDKey).(string)
	if !ok {
		id, ok = ctx.Value(string(AgentIDKey)).(string)
	}
	if !ok || id == "" {
		return "", domain.ErrUnauthorized
	}
	return id, nil
}
