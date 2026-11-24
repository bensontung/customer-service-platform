package workbench

import (
	"context"

	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/example/customer-service-platform/pkg/store"
	"github.com/zeromicro/go-zero/core/logx"
)

type ConversationsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewConversationsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ConversationsLogic {
	return &ConversationsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ConversationsLogic) Conversations(req *types.ConversationQuery) (*types.ConversationListResponse, error) {
	agent, err := CurrentAgent(l.ctx, l.svcCtx.Store)
	if err != nil {
		return nil, err
	}
	if req.Assignee == "me" {
		req.Assignee = agent.ID
	}
	if agent.Role == domain.RoleAgent && (req.Assignee == "" || req.Assignee != agent.ID && req.Assignee != "unassigned") {
		req.Assignee = agent.ID
	}
	if req.Assignee == "unassigned" {
		req.Assignee = ""
		req.Status = domain.StatusWaiting
	}
	items, err := l.svcCtx.Store.Conversations(l.ctx, store.ConversationFilter{Status: req.Status, Source: req.Source, Assignee: req.Assignee})
	if err != nil {
		return nil, err
	}
	resp := &types.ConversationListResponse{Items: []types.ConversationView{}}
	for _, item := range items {
		view := ConversationView(item)
		account, accountErr := l.svcCtx.Store.AccountByID(l.ctx, item.ChannelAccountID)
		if accountErr != nil {
			return nil, accountErr
		}
		view.Channel = account.Channel
		resp.Items = append(resp.Items, view)
	}
	return resp, nil
}
