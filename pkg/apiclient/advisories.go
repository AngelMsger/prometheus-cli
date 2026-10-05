package apiclient

// Advisory preserves response metadata without changing the existing slice
// return types of discovery methods. Query results also carry these fields.
// Endpoint is the API path only, without the server address or query values.
type Advisory struct {
	Endpoint string   `json:"endpoint"`
	Warnings []string `json:"warnings,omitempty"`
	Infos    []string `json:"infos,omitempty"`
}
