package app

import (
	"fmt"
	"strings"

	"flow/internal/flowdb"
	"flow/internal/harness"
	"flow/internal/harness/claude"
	"flow/internal/harness/codex"
)

func allHarnesses() []harness.Harness {
	return []harness.Harness{
		claude.New(),
		codex.New(),
	}
}

func registeredHarnessNames() string {
	names := make([]string, 0, len(allHarnesses()))
	for _, h := range allHarnesses() {
		names = append(names, string(h.Name()))
	}
	return strings.Join(names, ", ")
}

func harnessByName(name string) (harness.Harness, error) {
	normalized, err := flowdb.NormalizeHarnessName(name)
	if err != nil {
		return nil, err
	}
	for _, h := range allHarnesses() {
		if string(h.Name()) == normalized {
			return h, nil
		}
	}
	return nil, fmt.Errorf("task is pinned to harness %q, but this flow binary only supports: %s", normalized, registeredHarnessNames())
}

func harnessNameForProvider(provider string) (string, error) {
	normalized, err := flowdb.NormalizeSessionProvider(provider)
	if err != nil {
		return "", err
	}
	h, err := harnessByName(normalized)
	if err != nil {
		return "", err
	}
	return string(h.Name()), nil
}

func harnessForTask(task *flowdb.Task) (harness.Harness, error) {
	if task == nil {
		return harnessByName("")
	}
	if strings.TrimSpace(task.Harness) != "" {
		return harnessByName(task.Harness)
	}
	return harnessByName(task.SessionProvider)
}

func backgroundLauncherFor(h harness.Harness) (harness.BackgroundLauncher, error) {
	bg, ok := h.(harness.BackgroundLauncher)
	if !ok {
		return nil, fmt.Errorf("FLOW_TERM=bg requested a background agent, but harness %q does not support background sessions", h.Name())
	}
	return bg, nil
}
