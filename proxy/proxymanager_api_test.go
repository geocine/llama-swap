package proxy

import "testing"

func TestProxyManager_ConfiguredContextSize(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{
			name: "short flag",
			args: []string{"llama-server", "-m", "model.gguf", "-c", "262144"},
			want: 262144,
		},
		{
			name: "long flag",
			args: []string{"llama-server", "--ctx-size", "8192"},
			want: 8192,
		},
		{
			name: "equals flag",
			args: []string{"llama-server", "--ctx-size=16384"},
			want: 16384,
		},
		{
			name: "alternate llama flag",
			args: []string{"llama-server", "--n_ctx", "32768"},
			want: 32768,
		},
		{
			name: "missing value",
			args: []string{"llama-server", "--ctx-size"},
			want: 0,
		},
		{
			name: "invalid value",
			args: []string{"llama-server", "-c", "large"},
			want: 0,
		},
		{
			name: "absent",
			args: []string{"llama-server", "-m", "model.gguf"},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := configuredContextSize(func() ([]string, error) {
				return tt.args, nil
			})
			if got != tt.want {
				t.Fatalf("configuredContextSize() = %d, want %d", got, tt.want)
			}
		})
	}
}
