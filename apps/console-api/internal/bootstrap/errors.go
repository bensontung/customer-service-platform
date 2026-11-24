package bootstrap

import (
	"errors"

	workbenchlogic "github.com/example/customer-service-platform/apps/console-api/internal/logic/workbench"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func ConfigureErrorHandler() {
	httpx.SetErrorHandler(func(err error) (int, any) {
		status, message := workbenchlogic.HTTPError(err)
		return status, map[string]string{"code": ErrorCode(err), "message": message}
	})
}
func ErrorCode(err error) string {
	switch {
	case errors.Is(err, domain.ErrReplyExpired):
		return "WECHAT_REPLY_WINDOW_EXPIRED"
	case errors.Is(err, domain.ErrReplyQuota):
		return "WECHAT_REPLY_QUOTA_EXHAUSTED"
	case errors.Is(err, domain.ErrForbidden):
		return "FORBIDDEN"
	case errors.Is(err, domain.ErrConflict):
		return "CONFLICT"
	case errors.Is(err, domain.ErrNotFound):
		return "NOT_FOUND"
	default:
		return "REQUEST_FAILED"
	}
}
