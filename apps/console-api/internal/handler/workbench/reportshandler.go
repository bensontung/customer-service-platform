package workbench

import (
	"net/http"

	"github.com/example/customer-service-platform/apps/console-api/internal/logic/workbench"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func ReportsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := workbench.NewReportsLogic(r.Context(), svcCtx)
		resp, err := l.Reports()
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
