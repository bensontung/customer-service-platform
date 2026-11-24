package public

import (
	"context"
	"time"

	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/example/customer-service-platform/pkg/auth"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/zeromicro/go-zero/core/logx"
)

type DingTalkLoginLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDingTalkLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DingTalkLoginLogic {
	return &DingTalkLoginLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *DingTalkLoginLogic) DingTalkLogin(req *types.LoginRequest) (*types.SessionResponse, error) {
	if l.svcCtx.DingTalk == nil {
		return nil, domain.ErrUnauthorized
	}
	user, err := l.svcCtx.DingTalk.ExchangeLoginCode(l.ctx, req.Code)
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	agent, err := l.svcCtx.Store.AgentByDingTalkID(l.ctx, user.UserID)
	if err != nil || !agent.Active {
		return nil, domain.ErrForbidden
	}
	lifetime := time.Duration(l.svcCtx.Config.Auth.AccessExpire) * time.Second
	token, err := auth.Issue(l.svcCtx.Config.Auth.AccessSecret, agent, lifetime)
	if err != nil {
		return nil, err
	}
	return &types.SessionResponse{Token: token, AgentId: agent.ID, Role: agent.Role, Name: agent.Name}, nil
}
