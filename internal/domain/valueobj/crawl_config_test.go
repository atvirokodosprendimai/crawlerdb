package valueobj_test

import (
	"testing"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
	"github.com/stretchr/testify/assert"
)

func TestCrawlConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     valueobj.CrawlConfig
		wantErr bool
	}{
		{
			name: "valid same domain",
			cfg: valueobj.CrawlConfig{
				Scope:      valueobj.ScopeSameDomain,
				MaxDepth:   5,
				Extraction: valueobj.ExtractionStandard,
			},
		},
		{
			name: "valid with externals",
			cfg: valueobj.CrawlConfig{
				Scope:         valueobj.ScopeFollowExternals,
				MaxDepth:      10,
				ExternalDepth: 2,
				Extraction:    valueobj.ExtractionFull,
				AntiBotMode:   valueobj.AntiBotRotate,
			},
		},
		{
			name: "invalid scope",
			cfg: valueobj.CrawlConfig{
				Scope:      "bogus",
				Extraction: valueobj.ExtractionMinimal,
			},
			wantErr: true,
		},
		{
			name: "invalid extraction",
			cfg: valueobj.CrawlConfig{
				Scope:      valueobj.ScopeSameDomain,
				Extraction: "bogus",
			},
			wantErr: true,
		},
		{
			name: "invalid antibot",
			cfg: valueobj.CrawlConfig{
				Scope:       valueobj.ScopeSameDomain,
				Extraction:  valueobj.ExtractionMinimal,
				AntiBotMode: "bogus",
			},
			wantErr: true,
		},
		{
			name: "negative depth",
			cfg: valueobj.CrawlConfig{
				Scope:      valueobj.ScopeSameDomain,
				MaxDepth:   -1,
				Extraction: valueobj.ExtractionMinimal,
			},
			wantErr: true,
		},
		{
			name: "empty antibot is valid",
			cfg: valueobj.CrawlConfig{
				Scope:      valueobj.ScopeSameDomain,
				Extraction: valueobj.ExtractionMinimal,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
