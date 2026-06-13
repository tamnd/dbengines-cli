package dbengines

// System is the record for a database management system in the DB-Engines ranking.
type System struct {
	Rank   int    `json:"rank"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Score  string `json:"score"`
	Change string `json:"change"`
	URL    string `json:"url"`
}
