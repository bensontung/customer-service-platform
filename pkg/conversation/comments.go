package conversation

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/example/customer-service-platform/pkg/domain"
)

type CommentSource struct {
	AccountKey   string `json:"accountKey"`
	MsgDataID    string `json:"msgDataId"`
	ArticleIndex int    `json:"articleIndex"`
	Cursor       int    `json:"cursor"`
}
type CommentSyncer interface {
	SyncComments(context.Context, string, string, int, int) ([]domain.Comment, error)
}

func (w *Worker) SyncComments(ctx context.Context, client CommentSyncer, sources []CommentSource) error {
	for i := range sources {
		source := sources[i]
		for page := 0; page < 20; page++ {
			comments, err := client.SyncComments(ctx, source.AccountKey, source.MsgDataID, source.ArticleIndex, source.Cursor)
			if err != nil {
				return err
			}
			for _, comment := range comments {
				if err = w.Store.UpsertComment(ctx, comment); err != nil {
					return err
				}
			}
			source.Cursor += len(comments)
			if len(comments) < 49 {
				break
			}
		}
	}
	return nil
}
func CommentSources() []CommentSource {
	var sources []CommentSource
	_ = json.Unmarshal([]byte(os.Getenv("WECHAT_COMMENT_SOURCES_JSON")), &sources)
	return sources
}
func (w *Worker) RunCommentSync(ctx context.Context, client CommentSyncer, sources []CommentSource) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if len(sources) > 0 {
			_ = w.SyncComments(ctx, client, sources)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
