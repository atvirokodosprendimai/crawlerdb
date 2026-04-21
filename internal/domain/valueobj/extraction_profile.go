package valueobj

// ExtractionProfile defines what extractors to run for a given level.
type ExtractionProfile struct {
	Level         ExtractionLevel  `json:"level"`
	CustomPattern []string         `json:"custom_patterns,omitempty"` // CSS selectors or regex patterns
}

// IncludesHTML returns whether this profile stores full HTML.
func (e ExtractionProfile) IncludesHTML() bool {
	return e.Level == ExtractionStandard || e.Level == ExtractionFull
}

// IncludesText returns whether this profile extracts plain text.
func (e ExtractionProfile) IncludesText() bool {
	return e.Level == ExtractionFull
}

// IncludesStructuredData returns whether this profile extracts JSON-LD, OG, etc.
func (e ExtractionProfile) IncludesStructuredData() bool {
	return e.Level == ExtractionFull
}
