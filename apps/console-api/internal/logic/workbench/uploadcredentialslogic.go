package workbench

import (
	"context"
	"errors"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
	"time"
)

type UploadCredentialsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUploadCredentialsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UploadCredentialsLogic {
	return &UploadCredentialsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UploadCredentialsLogic) UploadCredentials(req *types.UploadCredentialsRequest) (*types.UploadCredentialsResponse, error) {
	if _, err := CurrentAgent(l.ctx, l.svcCtx.Store); err != nil {
		return nil, err
	}
	if l.svcCtx.Storage == nil {
		return nil, errors.New("object storage is not configured")
	}
	upload, err := l.svcCtx.Storage.PresignPut(l.ctx, req.Filename, req.ContentType, req.Size, 10*time.Minute)
	if err != nil {
		return nil, err
	}
	return &types.UploadCredentialsResponse{ObjectKey: upload.ObjectKey, UploadUrl: upload.UploadURL, Headers: upload.Headers, ExpiresAt: upload.ExpiresAt.Format(time.RFC3339)}, nil
}
