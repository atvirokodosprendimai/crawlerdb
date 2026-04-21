package valueobj

// NormalizedURL holds a URL after normalization with its hash.
type NormalizedURL struct {
	Raw        string `json:"raw"`
	Normalized string `json:"normalized"`
	Hash       string `json:"hash"` // SHA-256 hex
}
