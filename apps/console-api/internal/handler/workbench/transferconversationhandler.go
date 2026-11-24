package workbench

import (
	"net/http"

	"github.com/example/customer-service-platform/apps/console-api/internal/logic/workbench"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func TransferConversationHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.TransferRequest
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := workbench.NewTransferConversationLogic(r.Context(), svcCtx)
		resp, err := l.TransferConversation(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
