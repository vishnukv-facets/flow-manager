package codex

import "flow/internal/harness"

type codex struct{}

func New() harness.Harness {
	return &codex{}
}

func (c *codex) Name() harness.Name { return harness.NameCodex }

func (c *codex) Provider() string { return string(harness.NameCodex) }

func (c *codex) Binary() string { return "codex" }
