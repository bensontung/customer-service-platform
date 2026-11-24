package workbench

import (
	"context"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type CommentsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCommentsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CommentsLogic {
	return &CommentsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *CommentsLogic) Comments(req *types.CommentQuery) (*types.CommentListResponse, error) {
	if _, err := CurrentAgent(l.ctx, l.svcCtx.Store); err != nil {
		return nil, err
	}
	comments, err := l.svcCtx.Store.Comments(l.ctx, req.AccountId, req.Status)
	if err != nil {
		return nil, err
	}
	resp := &types.CommentListResponse{Items: []types.CommentView{}}
	for _, c := range comments {
		resp.Items = append(resp.Items, types.CommentView{Id: c.ID, MsgDataId: c.MsgDataID, ArticleIndex: c.ArticleIndex, CommentId: c.CommentID, Content: c.Content, Status: c.Status, ReplyContent: c.ReplyContent})
	}
	return resp, nil
}
