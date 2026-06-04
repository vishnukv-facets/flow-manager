package app

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"flow/internal/flowdb"
	"flow/internal/steering"
)

// cmdAttention implements `flow attention <list|act>` — the terminal surface
// for the attention router's feed (the Mission Control feed panel is P1.4).
func cmdAttention(args []string) int {
	if leadingHelpArg(args) || len(args) == 0 {
		printAttentionUsage()
		return 0
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return cmdAttentionList(rest)
	case "act":
		return cmdAttentionAct(rest)
	default:
		fmt.Fprintf(os.Stderr, "error: unknown attention subcommand %q (want list|act)\n", sub)
		printAttentionUsage()
		return 2
	}
}

func printAttentionUsage() {
	fmt.Println(`flow attention — review and act on the attention feed

  flow attention list [--status new|acted|dismissed|snoozed|all]   (default: new)
  flow attention act <id> <make-task|forward|dismiss>`)
}

func cmdAttentionList(args []string) int {
	fs := flagSet("attention list")
	status := fs.String("status", "new", "filter: new|acted|dismissed|snoozed|all")
	if handled, rc := parseFlagSet(fs, args); handled {
		return rc
	}
	db, rc := openAttentionDB()
	if rc != 0 {
		return rc
	}
	defer db.Close()

	filter := strings.TrimSpace(*status)
	if filter == "all" {
		filter = ""
	}
	items, err := flowdb.ListFeedItems(db, filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Print(renderAttentionFeed(items))
	return 0
}

func cmdAttentionAct(args []string) int {
	if leadingHelpArg(args) {
		printAttentionUsage()
		return 0
	}
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "error: act requires <id> and <make-task|forward|dismiss>")
		return 2
	}
	id, actionArg := args[0], strings.ToLower(strings.TrimSpace(args[1]))

	db, rc := openAttentionDB()
	if rc != 0 {
		return rc
	}
	defer db.Close()

	item, err := flowdb.GetFeedItem(db, id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: no feed item with id %q\n", id)
		return 1
	}

	switch actionArg {
	case "dismiss":
		if err := steering.DismissFeed(db, id); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Printf("dismissed %s\n", id)
		return 0
	case "make-task", "make_task":
		return runAttentionAction(db, item, steering.ActionMakeTask, "made task from")
	case "forward":
		return runAttentionAction(db, item, steering.ActionForward, "forwarded")
	default:
		fmt.Fprintf(os.Stderr, "error: unknown action %q (want make-task|forward|dismiss)\n", actionArg)
		return 2
	}
}

// runAttentionAction applies an operator-initiated (manual) feed action and
// reports the result. manual=true bypasses the autonomy gate — the operator
// at the terminal is the authorization.
func runAttentionAction(db *sql.DB, item flowdb.FeedItem, action steering.Action, verb string) int {
	if err := steering.ApplyAction(context.Background(), db, item, action, steering.DefaultAutonomy(), true); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("%s %s\n", verb, item.ID)
	return 0
}

func openAttentionDB() (*sql.DB, int) {
	dbPath, err := flowDBPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return nil, 1
	}
	db, err := flowdb.OpenDB(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return nil, 1
	}
	return db, 0
}

// renderAttentionFeed renders feed items as a compact table. Pure (no I/O) so
// it's unit-testable.
func renderAttentionFeed(items []flowdb.FeedItem) string {
	if len(items) == 0 {
		return "No attention items.\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-10s  %-7s  %-10s  %-5s  %-7s  %-14s  %s\n",
		"ID", "SOURCE", "ACTION", "CONF", "URGENCY", "MATCHED", "SUMMARY")
	for _, it := range items {
		matched := it.MatchedTask
		if matched == "" {
			matched = "-"
		}
		fmt.Fprintf(&b, "%-10s  %-7s  %-10s  %-5.2f  %-7s  %-14s  %s\n",
			shortID(it.ID), it.Source, it.SuggestedAction, it.Confidence,
			orDash(it.Urgency), matched, it.Summary)
	}
	return b.String()
}

func shortID(id string) string {
	if len(id) > 10 {
		return id[:10]
	}
	return id
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
