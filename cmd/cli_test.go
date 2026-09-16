package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun_DispatchSmoke(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "")
	t.Setenv("LLM_MODEL", "")

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout []string
		wantStderr []string
	}{
		{
			name:       "version command",
			args:       []string{"version"},
			wantCode:   0,
			wantStdout: []string{"llms ", "commit:", "built:"},
		},
		{
			name:       "version flag",
			args:       []string{"--version"},
			wantCode:   0,
			wantStdout: []string{"llms-cli version"},
		},
		{
			name:       "no args usage",
			args:       nil,
			wantCode:   0,
			wantStdout: []string{"llms-cli - CLI tool", "Usage:", "Commands:"},
		},
		{
			name:       "help usage",
			args:       []string{"-h"},
			wantCode:   0,
			wantStdout: []string{"llms-cli - CLI tool", "Usage:", "Commands:"},
		},
		{
			name:       "unknown command",
			args:       []string{"unknown"},
			wantCode:   1,
			wantStderr: []string{"llms-cli - CLI tool", "unknown command: unknown"},
		},
		{
			name:       "missing required provider flag",
			args:       []string{"chat", "hello"},
			wantCode:   1,
			wantStderr: []string{`required flag "provider" not set`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			gotCode := run(tt.args, &stdout, &stderr)
			if gotCode != tt.wantCode {
				t.Fatalf("run() exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", gotCode, tt.wantCode, stdout.String(), stderr.String())
			}
			for _, want := range tt.wantStdout {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
				}
			}
			for _, want := range tt.wantStderr {
				if !strings.Contains(stderr.String(), want) {
					t.Fatalf("stderr missing %q:\n%s", want, stderr.String())
				}
			}
		})
	}
}

// The runners take a writer, so nothing an action prints may go straight to
// os.Stdout: a caller that redirects output has to see all of it.
func TestProvidersAction_WritesToTheGivenWriter(t *testing.T) {
	var buf bytes.Buffer
	if err := runProviders(nil, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "openrouter") {
		t.Fatalf("provider listing did not reach the writer: %q", buf.String())
	}
}

// Every provider the listing advertises has to be constructible, or the CLI
// tells a user to pass a provider it then refuses.
func TestProvidersListing_MatchesCreateClient(t *testing.T) {
	var buf bytes.Buffer
	if err := runProviders(nil, &buf); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(buf.String(), "\n") {
		// Table rows are the indented ones; headings and the trailing notes are not.
		if !strings.HasPrefix(line, "  ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] == "PROVIDER" || fields[0] == "--------" {
			continue
		}
		if _, err := createClient(fields[0], ""); err != nil && strings.Contains(err.Error(), "unknown or non-chat provider") {
			t.Errorf("listed provider %q is not constructible", fields[0])
		}
	}
}
