package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func (s *Server) handleUIEvents(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, fmt.Errorf("streaming unsupported"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	writeEvent := func(name string, body []byte) bool {
		if _, err := fmt.Fprintf(w, "event: %s\n", name); err != nil {
			return false
		}
		if _, err := w.Write([]byte("data: ")); err != nil {
			return false
		}
		if _, err := w.Write(body); err != nil {
			return false
		}
		if _, err := w.Write([]byte("\n\n")); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	snapshot := func() ([]byte, []byte, error) {
		data, err := s.buildUIData()
		if err != nil {
			return nil, nil, err
		}
		body, err := json.Marshal(data)
		if err != nil {
			return nil, nil, err
		}
		fingerprint, err := uiDataStreamFingerprint(data)
		if err != nil {
			return nil, nil, err
		}
		return body, fingerprint, nil
	}

	var last []byte
	sendSnapshot := func(force bool) bool {
		body, fingerprint, err := snapshot()
		if err != nil {
			payload, _ := json.Marshal(map[string]string{"message": err.Error()})
			return writeEvent("ui-error", payload)
		}
		if !force && bytes.Equal(fingerprint, last) {
			return true
		}
		last = append(last[:0], fingerprint...)
		return writeEvent("ui-data", body)
	}

	if !sendSnapshot(true) {
		return
	}

	// Event-driven loop: instead of rebuilding the snapshot on a fixed
	// ticker (which burns CPU even when nothing changed), subscribe to
	// the in-process hub and rebuild only when something actually fires.
	// A short debounce coalesces bursts (e.g. PreToolUse / PostToolUse
	// pairs) into one rebuild; a long safety heartbeat catches anything
	// that mutated state without publishing.
	var sub *eventSubscriber
	if s.events != nil {
		sub = s.events.subscribe(eventFilter{})
		defer s.events.unsubscribe(sub)
	}
	var subCh <-chan eventEnvelope
	if sub != nil {
		subCh = sub.send
	}

	// Fast tick + dirty flag is a select-friendly debounce: events set
	// dirty=true, the tick checks and clears it. The tick fires often
	// but does nothing unless dirty, so idle cost is just a select
	// wakeup. 250ms balances UI snappiness against burst coalescing.
	debounce := time.NewTicker(250 * time.Millisecond)
	defer debounce.Stop()
	// Safety heartbeat: force a rebuild even with no events. CLI
	// mutations are caught by dbWatcher (PRAGMA data_version polling)
	// and UI mutations publish via handleAction, so this exists only as
	// a backstop for filesystem-side changes (brief/KB markdown edits)
	// and anything we forgot to wire. Keep generous so idle CPU stays
	// near zero.
	safety := time.NewTicker(30 * time.Second)
	defer safety.Stop()
	keepAlive := time.NewTicker(20 * time.Second)
	defer keepAlive.Stop()

	dirty := false
	for {
		select {
		case <-r.Context().Done():
			return
		case _, ok := <-subCh:
			if !ok {
				// Hub closed our subscriber; fall back to safety + keepalive.
				subCh = nil
				continue
			}
			dirty = true
		case <-debounce.C:
			if dirty {
				dirty = false
				if !sendSnapshot(false) {
					return
				}
			}
		case <-safety.C:
			dirty = false
			if !sendSnapshot(false) {
				return
			}
		case <-keepAlive.C:
			if _, err := w.Write([]byte(": keep-alive\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func uiDataStreamFingerprint(data uiData) ([]byte, error) {
	stable := data
	stable.Agents = append([]uiAgent(nil), data.Agents...)
	for i := range stable.Agents {
		stable.Agents[i].StartedMin = 0
		stable.Agents[i].LastActivitySec = 0
	}
	if data.DeadAgent != nil {
		dead := *data.DeadAgent
		dead.StartedMin = 0
		dead.LastActivitySec = 0
		stable.DeadAgent = &dead
	}
	stable.Playbooks = append([]uiPlaybook(nil), data.Playbooks...)
	for i := range stable.Playbooks {
		stable.Playbooks[i].LastMin = nil
	}
	stable.Workdirs = append([]uiWorkdir(nil), data.Workdirs...)
	for i := range stable.Workdirs {
		stable.Workdirs[i].UsedMin = 0
	}
	return json.Marshal(stable)
}
