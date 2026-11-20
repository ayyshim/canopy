package api

import (
	"context"
	"log/slog"
)

func httpxLogError(ctx context.Context, msg string, err error) {
	slog.ErrorContext(ctx, msg, "err", err)
}
