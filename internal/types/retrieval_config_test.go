package types

import "testing"

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
