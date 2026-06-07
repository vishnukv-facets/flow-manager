package workevents

import "testing"

func TestBucketOrdering(t *testing.T) {
	cases := []struct {
		a, b Bucket
		want Bucket
	}{
		{BucketFYI, BucketNeedsAction, BucketNeedsAction},
		{BucketWaiting, BucketCloseout, BucketCloseout},
		{BucketIgnored, BucketHandled, BucketHandled},
		{BucketNextUp, BucketFYI, BucketNextUp},
	}
	for _, c := range cases {
		if got := StrongerBucket(c.a, c.b); got != c.want {
			t.Errorf("StrongerBucket(%q, %q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}

func TestEventLinkValidation(t *testing.T) {
	valid := Link{Kind: "task", Target: "autonomy-trust-ladder"}
	if !valid.Valid() {
		t.Fatalf("task link should be valid: %+v", valid)
	}
	invalid := Link{Kind: "task", Target: ""}
	if invalid.Valid() {
		t.Fatalf("empty target link should be invalid: %+v", invalid)
	}
}

func TestFilterMatches(t *testing.T) {
	ev := Event{Source: "github", Bucket: BucketNeedsAction, TaskSlug: "autonomy-trust-ladder"}
	exact := Filter{Source: "github", Bucket: BucketNeedsAction, TaskSlug: "autonomy-trust-ladder"}
	if !exact.Matches(ev) {
		t.Fatal("exact filter should match")
	}
	differentSource := Filter{Source: "slack"}
	if differentSource.Matches(ev) {
		t.Fatal("different source should not match")
	}
}
