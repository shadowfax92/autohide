package daemon

import (
	"context"
	"time"
)

const (
	watchRetryMin = time.Second
	watchRetryMax = 30 * time.Second
)

// watchLoop supervises the optional helper stream without blocking snapshots.
func (d *Daemon) watchLoop(ctx context.Context) {
	backoff := watchRetryMin
	for {
		path, err := LocateHelper()
		if err == nil {
			started := time.Now()
			err = NewHelper(path).Watch(ctx, d.handleWatchEvent)
			d.clearWatchAwayState()
			if ctx.Err() != nil {
				return
			}
			if time.Since(started) >= watchRetryMax {
				backoff = watchRetryMin
			}
			d.logger.Warn().Err(err).Dur("retry_in", backoff).Msg("activity watcher stopped")
		} else {
			d.clearWatchAwayState()
			d.logger.Debug().Err(err).Dur("retry_in", backoff).Msg("activity watcher unavailable")
		}

		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
		backoff = min(backoff*2, watchRetryMax)
	}
}

func (d *Daemon) handleWatchEvent(event WatchEvent) {
	switch event.Type {
	case "activate":
		d.tracker.ActivateApp(event.Name, time.UnixMilli(event.TS))
	case "deactivate":
		d.tracker.TouchApp(event.Name, time.UnixMilli(event.TS))
	case "sleep":
		d.mu.Lock()
		d.sleeping = true
		d.mu.Unlock()
	case "wake":
		d.mu.Lock()
		d.sleeping = false
		d.mu.Unlock()
	case "lock":
		d.mu.Lock()
		d.locked = true
		d.mu.Unlock()
	case "unlock":
		d.mu.Lock()
		d.locked = false
		d.mu.Unlock()
	}
}

func (d *Daemon) clearWatchAwayState() {
	d.mu.Lock()
	d.locked = false
	d.sleeping = false
	d.mu.Unlock()
}
