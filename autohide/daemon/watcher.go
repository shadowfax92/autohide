package daemon

import (
	"context"
	"time"
)

const (
	watchRetryMin = time.Second
	watchRetryMax = 30 * time.Second
)

type awayInterval struct {
	Start time.Time
	End   time.Time
}

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
		d.tracker.ActivateApp(event.Pid, event.Name, time.UnixMilli(event.TS))
	case "deactivate":
		d.tracker.TouchApp(event.Name, time.UnixMilli(event.TS))
	case "sleep":
		d.setWatchAway("sleep", true, time.UnixMilli(event.TS))
	case "wake":
		d.setWatchAway("sleep", false, time.UnixMilli(event.TS))
	case "lock":
		d.setWatchAway("lock", true, time.UnixMilli(event.TS))
	case "unlock":
		d.setWatchAway("lock", false, time.UnixMilli(event.TS))
	}
}

func (d *Daemon) clearWatchAwayState() {
	d.mu.Lock()
	d.accumulateWatchAway(time.Now().Round(0))
	d.locked = false
	d.sleeping = false
	d.awayStartedAt = time.Time{}
	d.mu.Unlock()
}

func (d *Daemon) setWatchAway(kind string, active bool, at time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	wasAway := d.locked || d.sleeping
	if kind == "lock" {
		d.locked = active
	} else {
		d.sleeping = active
	}
	isAway := d.locked || d.sleeping
	if !wasAway && isAway {
		d.awayStartedAt = at
	} else if wasAway && !isAway {
		d.accumulateWatchAway(at)
		d.awayStartedAt = time.Time{}
	}
}

func (d *Daemon) accumulateWatchAway(until time.Time) {
	if !d.awayStartedAt.IsZero() && until.After(d.awayStartedAt) {
		d.awayIntervals = append(d.awayIntervals, awayInterval{Start: d.awayStartedAt, End: until})
	}
}

func (d *Daemon) consumeWatchAway(now time.Time) []awayInterval {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.accumulateWatchAway(now)
	intervals := d.awayIntervals
	d.awayIntervals = nil
	if d.locked || d.sleeping {
		d.awayStartedAt = now
	} else {
		d.awayStartedAt = time.Time{}
	}
	return intervals
}
