package server

import (
	"encoding/json"
	"errors"
	"flow/internal/flowdb"
	"net/http"
	"strings"
	"time"
)

type ownerNextRequest struct {
	In string `json:"in"`
	At string `json:"at"`
}

func (s *Server) handleOwners(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	if r.URL.Path != "/api/owners" {
		http.NotFound(w, r)
		return
	}
	owners, err := flowdb.ListOwners(s.cfg.DB, flowdb.OwnerFilter{
		Status:          r.URL.Query().Get("status"),
		IncludeArchived: boolQuery(r.URL.Query(), "include_archived"),
	})
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, BuildOwnerViews(s.cfg.DB, s.cfg.FlowRoot, owners))
}

func (s *Server) handleOwnerRoute(w http.ResponseWriter, r *http.Request) {
	parts, ok := routeParts(w, r, "/api/owners/")
	if !ok {
		return
	}
	if len(parts) == 0 {
		http.NotFound(w, r)
		return
	}
	slug := parts[0]
	if len(parts) == 1 {
		if !getOnly(w, r) {
			return
		}
		o, err := flowdb.GetOwner(s.cfg.DB, slug)
		if err != nil {
			writeNotFoundOrError(w, err)
			return
		}
		writeJSON(w, BuildOwnerView(s.cfg.DB, s.cfg.FlowRoot, o, time.Now()))
		return
	}
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	switch parts[1] {
	case "start":
		if err := flowdb.ActivateOwner(s.cfg.DB, slug, flowdb.NowISO()); err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
	case "pause":
		if err := flowdb.PauseOwner(s.cfg.DB, slug); err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
	case "retire":
		if err := flowdb.RetireOwner(s.cfg.DB, slug); err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
	case "next":
		next, err := ownerNextTime(r)
		if err != nil {
			writeError(w, err, http.StatusBadRequest)
			return
		}
		if err := flowdb.SetOwnerNextWake(s.cfg.DB, slug, next.Format(time.RFC3339)); err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
	case "tick":
		if out, err := s.runFlowCommand("owner", "tick", slug, "--auto"); err != nil {
			msg := strings.TrimSpace(out)
			if msg != "" {
				msg += ": "
			}
			writeError(w, errors.New(msg+err.Error()), http.StatusInternalServerError)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	o, err := flowdb.GetOwner(s.cfg.DB, slug)
	if err != nil {
		writeNotFoundOrError(w, err)
		return
	}
	writeJSON(w, BuildOwnerView(s.cfg.DB, s.cfg.FlowRoot, o, time.Now()))
}

func ownerNextTime(r *http.Request) (time.Time, error) {
	var req ownerNextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return time.Time{}, err
	}
	if (req.In == "") == (req.At == "") {
		return time.Time{}, errors.New("give exactly one of in or at")
	}
	if req.In != "" {
		d, err := time.ParseDuration(req.In)
		if err != nil {
			return time.Time{}, err
		}
		return time.Now().Add(d), nil
	}
	return time.Parse(time.RFC3339, req.At)
}
