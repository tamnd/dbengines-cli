package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

func (a *App) rankingCmd() *cobra.Command {
	var model string

	cmd := &cobra.Command{
		Use:   "ranking",
		Short: "Show the DB-Engines database ranking",
		Long: `Fetch and display the DB-Engines ranking of database management systems.

By default shows the top 20 systems. Use --limit to change the count
and --model to filter by database model type (e.g. Relational, Document).`,
		Example: `  dbe ranking
  dbe ranking --limit 50
  dbe ranking --model Relational
  dbe ranking -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			systems, err := a.client.Ranking(cmd.Context())
			if err != nil {
				return codeError(exitError, err)
			}

			if model != "" {
				filtered := systems[:0]
				ml := strings.ToLower(model)
				for _, s := range systems {
					if strings.Contains(strings.ToLower(s.Type), ml) {
						filtered = append(filtered, s)
					}
				}
				systems = filtered
			}

			limit := a.effectiveLimit(20)
			if limit > 0 && limit < len(systems) {
				systems = systems[:limit]
			}

			return a.renderOrEmpty(systems, len(systems))
		},
	}

	cmd.Flags().StringVar(&model, "model", "", "filter by database model type (e.g. Relational, Document, Graph)")
	return cmd
}
