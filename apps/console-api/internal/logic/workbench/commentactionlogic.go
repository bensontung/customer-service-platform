package workbench

import (
	"context"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/example/customer-service-platform/pkg/store"
	"github.com/zeromicro/go-zero/core/logx"
)

type CommentActionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCommentActionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CommentActionLogic {
	return &CommentActionLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *CommentActionLogic) CommentAction(req *types.CommentActionRequest) (*types.IdResponse, error) {
	agent, err := CurrentAgent(l.ctx, l.svcCtx.Store)
	if err != nil {
		return nil, err
	}
	comment, err := l.svcCtx.Store.Comment(l.ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if l.svcCtx.WeChat == nil {
		return nil, domain.ErrInvalidRequest
	}
	if err = l.svcCtx.WeChat.CommentAction(l.ctx, comment, req.Action, req.Content); err != nil {
		return nil, err
	}
	switch req.Action {
	case "reply":
		comment.ReplyContent = req.Content
	case "delete":
		comment.Status = "deleted"
	case "feature":
		comment.Status = "featured"
	case "unfeature":
		comment.Status = "normal"
	}
	if err = l.svcCtx.Store.UpdateComment(l.ctx, comment); err != nil {
		return nil, err
	}
	if err = l.svcCtx.Store.Audit(l.ctx, domain.AuditLog{ID: domain.NewID(), ActorID: agent.ID, Action: "comment." + req.Action, ResourceType: "comment", ResourceID: comment.ID, Detail: store.JSON(map[string]string{"msgDataId": comment.MsgDataID})}); err != nil {
		return nil, err
	}
	return &types.IdResponse{Id: comment.ID}, nil
}
