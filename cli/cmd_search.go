package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func (a *App) searchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the ranking by database name",
		Long: `Fetch the DB-Engines ranking and filter results by name.

The search is case-insensitive and matches any part of the database name.`,
		Example: `  dbe search postgres
  dbe search "sql server" --limit 5
  dbe search mongo -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			if query == "" {
				return codeError(exitUsage, fmt.Errorf("search query must not be empty"))
			}

			systems, err := a.client.Ranking(cmd.Context())
			if err != nil {
				return codeError(exitError, err)
			}

			ql := strings.ToLower(query)
			filtered := systems[:0]
			for _, s := range systems {
				if strings.Contains(strings.ToLower(s.Name), ql) {
					filtered = append(filtered, s)
				}
			}
			systems = filtered

			limit := a.effectiveLimit(10)
			if limit > 0 && limit < len(systems) {
				systems = systems[:limit]
			}

			return a.renderOrEmpty(systems, len(systems))
		},
	}
	return cmd
}
