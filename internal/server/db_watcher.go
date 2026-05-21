package server

import (
	"context"
	"time"
)

// dbWatcher polls SQLite's `PRAGMA data_version` to detect writes
// performed by *other* database connections — in practice, mutations
// from a separate flow CLI process (e.g. `flow add task`, `flow done`).
// When a change is observed it publishes a ui_change event so any
// /api/events subscriber rebuilds promptly.
//
// SQLite increments data_version only for writes from *other*
// connections, so in-process server mutations don't double-fire here —
// they already call publishUIChange directly from the action handler.
// The poll itself is a single PRAGMA query (microseconds), so a 1s
// interval is effectively free.
//
// We pin a dedicated *sql.Conn for the watcher rather than letting
// each tick check out a random pool connection. data_version is read
// from the connection's view of the database file header, and using a
// stable connection avoids spurious increments caused by pool churn.
type dbWatcher struct {
	srv      *Server
	interval time.Duration
	stop     chan struct{}
	done     chan struct{}
}

const defaultDBWatchInterval = 1 * time.Second

func newDBWatcher(srv *Server) *dbWatcher {
	return &dbWatcher{
		srv:      srv,
		interval: defaultDBWatchInterval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

func (w *dbWatcher) start() {
	if w == nil || w.srv == nil || w.srv.cfg.DB == nil {
		close(w.done)
		return
	}
	go w.loop()
}

func (w *dbWatcher) stopWatching() {
	if w == nil {
		return
	}
	select {
	case <-w.stop:
		return
	default:
		close(w.stop)
	}
	<-w.done
}

func (w *dbWatcher) loop() {
	defer close(w.done)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, err := w.srv.cfg.DB.Conn(ctx)
	if err != nil {
		return
	}
	defer conn.Close()

	var last int64
	if err := conn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&last); err != nil {
		return
	}

	tick := time.NewTicker(w.interval)
	defer tick.Stop()

	for {
		select {
		case <-w.stop:
			return
		case <-tick.C:
			var cur int64
			if err := conn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&cur); err != nil {
				// Transient errors (e.g. lock contention) are fine —
				// retry on the next tick. A persistent error means the
				// DB is gone, in which case the server is on its way
				// down anyway.
				continue
			}
			if cur != last {
				last = cur
				w.srv.publishUIChange("db")
			}
		}
	}
}

