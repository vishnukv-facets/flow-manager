package server

import (
	"net/http"
	"strconv"
	"strings"

	"flow/internal/workevents"
)

func (s *Server) handleWorkEvents(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	filter := workevents.Filter{
		Source:   strings.TrimSpace(r.URL.Query().Get("source")),
		Bucket:   workevents.Bucket(strings.TrimSpace(r.URL.Query().Get("bucket"))),
		TaskSlug: strings.TrimSpace(r.URL.Query().Get("task")),
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			filter.Limit = n
		}
	}
	result, err := workevents.Build(s.cfg.DB, s.cfg.FlowRoot, filter)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, result)
}
