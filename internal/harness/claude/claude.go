package claude

import "flow/internal/harness"

type claude struct{}

func New() harness.Harness {
	return &claude{}
}

func (c *claude) Name() harness.Name { return harness.NameClaude }

func (c *claude) Provider() string { return string(harness.NameClaude) }

func (c *claude) Binary() string { return "claude" }
