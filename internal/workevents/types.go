package workevents

import "strings"

type Bucket string

const (
	BucketNeedsAction Bucket = "needs_action"
	BucketCloseout    Bucket = "closeout"
	BucketWaiting     Bucket = "waiting"
	BucketNextUp      Bucket = "next_up"
	BucketFYI         Bucket = "fyi"
	BucketHandled     Bucket = "handled"
	BucketIgnored     Bucket = "ignored"
)

type Link struct {
	Kind   string `json:"kind"`
	Label  string `json:"label,omitempty"`
	Target string `json:"target"`
	URL    string `json:"url,omitempty"`
}

func (l Link) Valid() bool {
	return strings.TrimSpace(l.Kind) != "" && strings.TrimSpace(l.Target) != ""
}

type Event struct {
	ID             string  `json:"id"`
	Source         string  `json:"source"`
	Kind           string  `json:"kind"`
	EventKey       string  `json:"event_key,omitempty"`
	ThreadKey      string  `json:"thread_key,omitempty"`
	URL            string  `json:"url,omitempty"`
	Title          string  `json:"title"`
	Summary        string  `json:"summary,omitempty"`
	Actor          string  `json:"actor,omitempty"`
	AuthoredBySelf bool    `json:"authored_by_self,omitempty"`
	OccurredAt     string  `json:"occurred_at,omitempty"`
	ObservedAt     string  `json:"observed_at,omitempty"`
	TaskSlug       string  `json:"task_slug,omitempty"`
	ProjectSlug    string  `json:"project_slug,omitempty"`
	EntityKind     string  `json:"entity_kind,omitempty"`
	EntityRef      string  `json:"entity_ref,omitempty"`
	Bucket         Bucket  `json:"bucket"`
	Urgency        string  `json:"urgency,omitempty"`
	Confidence     float64 `json:"confidence,omitempty"`
	ReasonCode     string  `json:"reason_code,omitempty"`
	ReasonText     string  `json:"reason_text,omitempty"`
	Links          []Link  `json:"links,omitempty"`
}

type Counts struct {
	NeedsAction int `json:"needs_action"`
	Closeout    int `json:"closeout"`
	Waiting     int `json:"waiting"`
	NextUp      int `json:"next_up"`
	FYI         int `json:"fyi"`
	Handled     int `json:"handled"`
	Ignored     int `json:"ignored"`
}

type Result struct {
	Items  []Event `json:"items"`
	Counts Counts  `json:"counts"`
}

type Filter struct {
	Source   string
	Bucket   Bucket
	TaskSlug string
	Limit    int
}

func (f Filter) Matches(ev Event) bool {
	if f.Source != "" && ev.Source != f.Source {
		return false
	}
	if f.Bucket != "" && ev.Bucket != f.Bucket {
		return false
	}
	if f.TaskSlug != "" && ev.TaskSlug != f.TaskSlug {
		return false
	}
	return true
}

func StrongerBucket(a, b Bucket) Bucket {
	if bucketRank(a) <= bucketRank(b) {
		return a
	}
	return b
}

func Count(items []Event) Counts {
	var c Counts
	for _, it := range items {
		switch it.Bucket {
		case BucketNeedsAction:
			c.NeedsAction++
		case BucketCloseout:
			c.Closeout++
		case BucketWaiting:
			c.Waiting++
		case BucketNextUp:
			c.NextUp++
		case BucketFYI:
			c.FYI++
		case BucketHandled:
			c.Handled++
		case BucketIgnored:
			c.Ignored++
		}
	}
	return c
}

func bucketRank(b Bucket) int {
	switch b {
	case BucketNeedsAction:
		return 0
	case BucketCloseout:
		return 1
	case BucketWaiting:
		return 2
	case BucketNextUp:
		return 3
	case BucketFYI:
		return 4
	case BucketHandled:
		return 5
	case BucketIgnored:
		return 6
	default:
		return 7
	}
}
