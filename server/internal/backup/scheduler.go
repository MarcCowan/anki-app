package backup

import (
	"context"
	"time"
)

// RunScheduler checks whether a daily backup is due at once and then every
// `every`, until ctx is done. Failures are logged at ERROR and it keeps going.
func (w *Writer) RunScheduler(ctx context.Context, every time.Duration) {
	w.tick(ctx)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

func (w *Writer) tick(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	existing, err := List(w.dir(Daily))
	if err != nil {
		w.Log.Error("scheduled backup: list failed", "err", err)
		return
	}
	if !Due(w.Clock.Now(), w.Loc, existing) {
		return
	}
	if _, err := w.Write(ctx, Daily); err != nil {
		if ctx.Err() != nil {
			w.Log.Info("scheduled backup interrupted by shutdown")
			return
		}
		w.Log.Error("scheduled backup failed", "err", err)
	}
}
