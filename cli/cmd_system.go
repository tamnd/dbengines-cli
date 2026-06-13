package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func (a *App) systemCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "system <name>",
		Short: "Show details about a specific database system",
		Long: `Look up a database management system by name in the ranking.

The search is case-insensitive and returns the first matching entry.`,
		Example: `  dbe system oracle
  dbe system postgresql
  dbe system "sql server" -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if name == "" {
				return codeError(exitUsage, fmt.Errorf("system name must not be empty"))
			}

			systems, err := a.client.Ranking(cmd.Context())
			if err != nil {
				return codeError(exitError, err)
			}

			nl := strings.ToLower(name)
			for _, s := range systems {
				if strings.ToLower(s.Name) == nl {
					return a.render([]any{s})
				}
			}
			// Fallback: partial match.
			for _, s := range systems {
				if strings.Contains(strings.ToLower(s.Name), nl) {
					return a.render([]any{s})
				}
			}

			return codeError(exitNoData, fmt.Errorf("system %q not found in ranking", name))
		},
	}
	return cmd
}
