package public

import "context"

type PublicNotifier interface {
	SendServiceText(ctx context.Context, text string) (messageID int, err error)
	EditServiceText(ctx context.Context, messageID int, text string) error
}
