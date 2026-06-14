package schedule

import (
	"testing"
	"time"
)

func TestParseEnglish(t *testing.T) {
	cases := []struct {
		in       string
		wantCron string
		wantKind string
	}{
		// presets + synonyms
		{"every hour", "@hourly", KindPreset},
		{"Hourly", "@hourly", KindPreset},
		{"once an hour", "@hourly", KindPreset},
		{"every day", "@daily", KindPreset},
		{"DAILY", "@daily", KindPreset},
		{"weekly", "@weekly", KindPreset},
		{"weekly once", "@weekly", KindPreset},
		{"once a week", "@weekly", KindPreset},
		// every N
		{"every 6 hours", "@every 6h", KindEvery},
		{"every 1 hour", "@hourly", KindPreset}, // special-cased to @hourly
		{"every 2 hrs", "@every 2h", KindEvery},
		{"every 30 minutes", "@every 30m", KindEvery},
		{"every 15 min", "@every 15m", KindEvery},
		{"every minute", "@every 1m", KindEvery},
		// day-and-time
		{"Wednesday at 1pm", "0 13 * * 3", KindDaytime},
		{"wednesday at 1pm", "0 13 * * 3", KindDaytime},
		{"wednesdays at 1pm", "0 13 * * 3", KindDaytime},
		{"every monday at 9am", "0 9 * * 1", KindDaytime},
		{"on friday at 5:30pm", "30 17 * * 5", KindDaytime},
		{"sunday at midnight", "0 0 * * 0", KindDaytime},
		{"saturday at noon", "0 12 * * 6", KindDaytime},
		{"tue at 13:45", "45 13 * * 2", KindDaytime},
		{"every day at 9am", "0 9 * * *", KindDaytime},
		{"daily at 18:00", "0 18 * * *", KindDaytime},
		// raw cron passthrough
		{"0 13 * * 1-5", "0 13 * * 1-5", KindCron},
		{"@every 90m", "@every 90m", KindCron},
		{"*/10 * * * *", "*/10 * * * *", KindCron},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := Parse(c.in)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", c.in, err)
			}
			if got.Cron != c.wantCron {
				t.Errorf("Parse(%q).Cron = %q, want %q", c.in, got.Cron, c.wantCron)
			}
			if got.Kind != c.wantKind {
				t.Errorf("Parse(%q).Kind = %q, want %q", c.in, got.Kind, c.wantKind)
			}
			if got.Input != c.in {
				t.Errorf("Parse(%q).Input = %q, want verbatim", c.in, got.Input)
			}
		})
	}
}

func TestParseRejects(t *testing.T) {
	bad := []string{
		"",
		"   ",
		"every 0 hours",
		"sometime next week",
		"wednesday at 25pm",
		"monday at 99:99",
		"every banana",
		"thursday", // no time
		"at 5pm",   // no day
	}
	for _, in := range bad {
		t.Run(in, func(t *testing.T) {
			if _, err := Parse(in); err == nil {
				t.Errorf("Parse(%q) = nil error, want error", in)
			}
		})
	}
}

func TestNext(t *testing.T) {
	// Fixed reference: 2026-06-14 is a Sunday.
	base := time.Date(2026, 6, 14, 10, 30, 0, 0, time.Local)

	t.Run("every 6h adds duration", func(t *testing.T) {
		got, err := Next("@every 6h", base)
		if err != nil {
			t.Fatal(err)
		}
		want := base.Add(6 * time.Hour)
		if !got.Equal(want) {
			t.Errorf("Next(@every 6h) = %v, want %v", got, want)
		}
	})

	t.Run("hourly rolls to next hour boundary", func(t *testing.T) {
		got, err := Next("@hourly", base)
		if err != nil {
			t.Fatal(err)
		}
		want := time.Date(2026, 6, 14, 11, 0, 0, 0, time.Local)
		if !got.Equal(want) {
			t.Errorf("Next(@hourly) = %v, want %v", got, want)
		}
	})

	t.Run("wednesday 1pm finds next wednesday", func(t *testing.T) {
		spec, err := Parse("Wednesday at 1pm")
		if err != nil {
			t.Fatal(err)
		}
		got, err := Next(spec.Cron, base)
		if err != nil {
			t.Fatal(err)
		}
		// From Sunday 2026-06-14, next Wednesday is 2026-06-17 13:00 local.
		want := time.Date(2026, 6, 17, 13, 0, 0, 0, time.Local)
		if !got.Equal(want) {
			t.Errorf("Next(Wed 1pm) = %v, want %v", got, want)
		}
	})

	t.Run("invalid spec errors", func(t *testing.T) {
		if _, err := Next("not a cron", base); err == nil {
			t.Error("Next(invalid) = nil error, want error")
		}
	})
}

func TestValidate(t *testing.T) {
	if err := Validate("@every 6h"); err != nil {
		t.Errorf("Validate(@every 6h) = %v, want nil", err)
	}
	if err := Validate("0 13 * * 3"); err != nil {
		t.Errorf("Validate(cron) = %v, want nil", err)
	}
	if err := Validate("garbage"); err == nil {
		t.Error("Validate(garbage) = nil, want error")
	}
}

func TestDescribe(t *testing.T) {
	if got := Describe(Spec{Input: "every 6 hours", Cron: "@every 6h"}); got != "every 6 hours" {
		t.Errorf("Describe prefers Input, got %q", got)
	}
	if got := Describe(Spec{Cron: "@every 6h"}); got != "@every 6h" {
		t.Errorf("Describe falls back to Cron, got %q", got)
	}
}
