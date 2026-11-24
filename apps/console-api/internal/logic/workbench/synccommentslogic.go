package workbench

import (
	"context"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type SyncCommentsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSyncCommentsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SyncCommentsLogic {
	return &SyncCommentsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *SyncCommentsLogic) SyncComments(req *types.SyncCommentRequest) (*types.IdResponse, error) {
	agent, err := CurrentAgent(l.ctx, l.svcCtx.Store)
	if err != nil {
		return nil, err
	}
	if err = RequireAdmin(agent); err != nil {
		return nil, err
	}
	comments, err := l.svcCtx.WeChat.SyncComments(l.ctx, req.AccountKey, req.MsgDataId, req.ArticleIndex, req.Begin)
	if err != nil {
		return nil, err
	}
	for _, comment := range comments {
		if err = l.svcCtx.Store.UpsertComment(l.ctx, comment); err != nil {
			return nil, err
		}
	}
	return &types.IdResponse{Id: req.MsgDataId}, nil
}
