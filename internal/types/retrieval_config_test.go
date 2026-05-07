package types

import "testing"

func TestGetEffectiveOverRetrieveMultiplier(t *testing.T) {
	tests := []struct {
		name string
		cfg  *RetrievalConfig
		want int
	}{
		{name: "nil returns default 5", cfg: nil, want: 5},
		{name: "zero returns default 5", cfg: &RetrievalConfig{OverRetrieveMultiplier: 0}, want: 5},
		{name: "negative returns default 5", cfg: &RetrievalConfig{OverRetrieveMultiplier: -3}, want: 5},
		{name: "positive value returned as-is", cfg: &RetrievalConfig{OverRetrieveMultiplier: 10}, want: 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetEffectiveOverRetrieveMultiplier(); got != tt.want {
				t.Errorf("GetEffectiveOverRetrieveMultiplier() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetEffectiveRerankModelID(t *testing.T) {
	tests := []struct {
		name            string
		cfg             *RetrievalConfig
		tenantDefaultID string
		want            string
	}{
		{
			name:            "config override wins over tenant default",
			cfg:             &RetrievalConfig{RerankModelID: "kb-rerank"},
			tenantDefaultID: "tenant-default",
			want:            "kb-rerank",
		},
		{
			name:            "empty config falls back to tenant default",
			cfg:             &RetrievalConfig{},
			tenantDefaultID: "tenant-default",
			want:            "tenant-default",
		},
		{
			name:            "nil config falls back to tenant default",
			cfg:             nil,
			tenantDefaultID: "tenant-default",
			want:            "tenant-default",
		},
		{
			name:            "no override and no tenant default returns empty",
			cfg:             &RetrievalConfig{},
			tenantDefaultID: "",
			want:            "",
		},
		{
			name:            "nil config and empty tenant default returns empty",
			cfg:             nil,
			tenantDefaultID: "",
			want:            "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetEffectiveRerankModelID(tt.tenantDefaultID); got != tt.want {
				t.Errorf("GetEffectiveRerankModelID(%q) = %q, want %q", tt.tenantDefaultID, got, tt.want)
			}
		})
	}
}
