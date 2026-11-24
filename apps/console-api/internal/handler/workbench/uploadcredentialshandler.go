package workbench

import (
	"net/http"

	"github.com/example/customer-service-platform/apps/console-api/internal/logic/workbench"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func UploadCredentialsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.UploadCredentialsRequest
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := workbench.NewUploadCredentialsLogic(r.Context(), svcCtx)
		resp, err := l.UploadCredentials(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
