package helpers

// VersionResponse represents version information response from GET /version.
// Fields match the handler in internal/handlers/operations/operations.go.
type VersionResponse struct {
	Service   string `json:"service"`
	Version   string `json:"version"`
	BuildDate string `json:"build_date"`
	Commit    string `json:"commit"`
}
