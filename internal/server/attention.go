package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"flow/internal/flowdb"
	"flow/internal/monitor"
	"flow/internal/steering"
)

// handleAttention serves GET /api/attention[?status=new|acted|dismissed|all]
// (default: new). 'all' returns every row.
func (s *Server) handleAttention(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "new"
	}
	if status == "all" {
		status = ""
	}
	items, err := flowdb.ListFeedItems(s.cfg.DB, status)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	views := make([]AttentionItemView, 0, len(items))
	for _, it := range items {
		views = append(views, attentionItemView(it))
	}
	writeJSON(w, views)
}

func attentionItemView(it flowdb.FeedItem) AttentionItemView {
	return AttentionItemView{
		ID: it.ID, Source: it.Source, ThreadKey: it.ThreadKey, Summary: it.Summary,
		SuggestedAction: it.SuggestedAction, MatchedTask: it.MatchedTask,
		SuggestedProject: it.SuggestedProject, SuggestedPriority: it.SuggestedPriority,
		Urgency: it.Urgency, IsVIP: it.IsVIP, Confidence: it.Confidence,
		Draft: it.Draft, Reason: it.Reason, Status: it.Status,
		CreatedAt: it.CreatedAt, ActedAt: it.ActedAt,
	}
}

// attentionAct handles the attention-act action: make-task | forward | dismiss
// on a feed item (Target = feed id). Operator-initiated → manual=true bypasses
// the autonomy gate.
func (s *Server) attentionAct(req actionRequest) (actionResponse, int) {
	id := strings.TrimSpace(req.Target)
	if id == "" {
		return actionResponse{OK: false, Message: "attention-act requires a feed item id (target)"}, http.StatusBadRequest
	}
	item, err := flowdb.GetFeedItem(s.cfg.DB, id)
	if err != nil {
		return actionResponse{OK: false, Message: "feed item not found: " + id}, http.StatusNotFound
	}
	switch strings.ToLower(strings.TrimSpace(req.AttentionAction)) {
	case "dismiss":
		if err := steering.DismissFeed(s.cfg.DB, id); err != nil {
			return actionResponse{OK: false, Message: err.Error()}, http.StatusInternalServerError
		}
		return actionResponse{OK: true, Message: "dismissed " + id}, http.StatusOK
	case "make-task", "make_task":
		if err := steering.ApplyAction(context.Background(), s.cfg.DB, item, steering.ActionMakeTask, steering.DefaultAutonomy(), true); err != nil {
			return actionResponse{OK: false, Message: err.Error()}, http.StatusInternalServerError
		}
		return actionResponse{OK: true, Message: "made task from " + id}, http.StatusOK
	case "forward":
		if err := steering.ApplyAction(context.Background(), s.cfg.DB, item, steering.ActionForward, steering.DefaultAutonomy(), true); err != nil {
			return actionResponse{OK: false, Message: err.Error()}, http.StatusInternalServerError
		}
		return actionResponse{OK: true, Message: "forwarded " + id}, http.StatusOK
	default:
		return actionResponse{OK: false, Message: "unknown attention action: " + req.AttentionAction}, http.StatusBadRequest
	}
}

// handleAttentionTrace serves GET /api/attention/trace?since=&disposition=&limit=
// — the steering decision-log funnel + recent trace rows. Defaults: since = 24h
// ago, disposition = all, limit = 200.
func (s *Server) handleAttentionTrace(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	q := r.URL.Query()
	since := strings.TrimSpace(q.Get("since"))
	if since == "" {
		since = time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	}
	disposition := strings.TrimSpace(q.Get("disposition"))
	if disposition == "all" {
		disposition = ""
	}
	limit := 200
	if n, err := strconv.Atoi(strings.TrimSpace(q.Get("limit"))); err == nil && n > 0 {
		limit = n
	}
	funnel, err := flowdb.SteeringFunnelSince(s.cfg.DB, since)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	traces, err := flowdb.ListSteeringTrace(s.cfg.DB, flowdb.TraceFilter{Disposition: disposition, Since: since, Limit: limit})
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	resp := AttentionTraceResponse{Funnel: steeringFunnelView(funnel), Items: make([]SteeringTraceView, 0, len(traces))}
	for _, t := range traces {
		resp.Items = append(resp.Items, s.steeringTraceView(r.Context(), t))
	}
	writeJSON(w, resp)
}

func steeringFunnelView(f flowdb.SteeringFunnel) SteeringFunnelView {
	return SteeringFunnelView{
		Observed:      f.Observed,
		DroppedStage0: f.DroppedStage0,
		DroppedCache:  f.DroppedCache,
		DroppedStage1: f.DroppedStage1,
		DroppedStage2: f.DroppedStage2,
		Surfaced:      f.Surfaced,
		Errors:        f.Errors,
	}
}

func (s *Server) steeringTraceView(ctx context.Context, t flowdb.SteeringTrace) SteeringTraceView {
	v := SteeringTraceView{
		ID: t.ID, CreatedAt: t.CreatedAt, Origin: t.Origin, Source: t.Source,
		Channel: t.Channel, ChannelType: t.ChannelType, Author: t.Author, ThreadKey: t.ThreadKey,
		TextPreview: t.TextPreview, Disposition: t.Disposition, StageReached: t.StageReached, DropReason: t.DropReason,
		Stage1Relevant: t.Stage1Relevant, Stage2Action: t.Stage2Action, Stage2Confidence: t.Stage2Confidence,
		Stage3Action: t.Stage3Action, Stage3Confidence: t.Stage3Confidence, FinalAction: t.FinalAction,
		FinalConfidence: t.FinalConfidence, FeedItemID: t.FeedItemID, Error: t.Error, LatencyMS: t.LatencyMS, Model: t.Model,
		TS: t.TS, TeamID: t.TeamID, URL: t.URL,
	}
	if t.Source == "github" {
		// GitHub fields are already human: owner/repo channel, GitHub login
		// author, the item URL is the canonical permalink. No resolver needed.
		v.ChannelName = t.Channel
		v.AuthorName = t.Author
		v.Text = t.TextPreview
		v.Permalink = t.URL
	} else if s.nameResolver != nil {
		v.ChannelName = s.nameResolver.ChannelName(ctx, t.Channel)
		v.AuthorName = s.nameResolver.UserName(ctx, t.Author)
		v.Text = s.nameResolver.CleanText(ctx, t.TextPreview)
	}
	if v.Text == "" {
		v.Text = t.TextPreview
	}
	if v.Permalink == "" {
		v.Permalink = steeringPermalink(t)
	}
	return v
}

// steeringPermalink builds a best-effort slack:// deep link to the traced
// message (team + channel + ts). Empty when team/channel/ts aren't all known.
func steeringPermalink(t flowdb.SteeringTrace) string {
	team := strings.TrimSpace(t.TeamID)
	channel := strings.TrimSpace(t.Channel)
	ts := strings.TrimSpace(t.TS)
	if t.Source == "slack" && team != "" && channel != "" && ts != "" {
		return fmt.Sprintf("slack://channel?team=%s&id=%s&message=%s", team, channel, ts)
	}
	return ""
}

// listSlackChannelsFn is the mockable seam for the channel-list endpoint.
var listSlackChannelsFn = monitor.ListSlackChannels

// handleSlackChannels serves GET /api/slack/channels — the channel list for
// the steering watch-channel picker.
func (s *Server) handleSlackChannels(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	channels, err := listSlackChannelsFn(r.Context())
	if err != nil {
		writeError(w, err, http.StatusBadGateway)
		return
	}
	if channels == nil {
		channels = []monitor.SlackChannelInfo{}
	}
	writeJSON(w, channels)
}
