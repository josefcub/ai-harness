package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeTestINI(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.ini")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test ini: %v", err)
	}
	return path
}

func TestLoadFullConfig(t *testing.T) {
	content := `
[server]
host = 0.0.0.0
port = 9090
webhook_path = "/api/hook"

[llm]
endpoint = "http://localhost:8080/v1"
model = "llama-3.1-8b"
api_key = "sk-test-key"
context_tokens = 4096
max_tokens = 2048
timeout = 60
max_tool_iterations = 10
system_prompt = "You are a test assistant."

[queue]
max_depth = 32

[paths]
working_dir = ./sandbox/
log_dir = ./logs/
state_dir = ./state/

[logging]
level = debug
log_tool_calls = true
log_agent_reasoning = true
log_channel_events = true
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Validate server
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("host = %q, want 0.0.0.0", cfg.Server.Host)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("port = %d, want 9090", cfg.Server.Port)
	}
	if cfg.Server.WebhookPath != "/api/hook" {
		t.Errorf("webhook_path = %q, want /api/hook", cfg.Server.WebhookPath)
	}

	// Validate LLM
	if cfg.LLM.Endpoint != "http://localhost:8080/v1" {
		t.Errorf("endpoint = %q, want http://localhost:8080/v1", cfg.LLM.Endpoint)
	}
	if cfg.LLM.Model != "llama-3.1-8b" {
		t.Errorf("model = %q, want llama-3.1-8b", cfg.LLM.Model)
	}
	if cfg.LLM.APIKey != "sk-test-key" {
		t.Errorf("api_key = %q, want sk-test-key", cfg.LLM.APIKey)
	}
	if cfg.LLM.ContextTokens != 4096 {
		t.Errorf("context_tokens = %d, want 4096", cfg.LLM.ContextTokens)
	}
	if cfg.LLM.MaxTokens != 2048 {
		t.Errorf("max_tokens = %d, want 2048", cfg.LLM.MaxTokens)
	}
	if cfg.LLM.Timeout != 60*time.Second {
		t.Errorf("timeout = %v, want 60s", cfg.LLM.Timeout)
	}
	if cfg.LLM.MaxToolIterations != 10 {
		t.Errorf("max_tool_iterations = %d, want 10", cfg.LLM.MaxToolIterations)
	}
	if cfg.LLM.SystemPrompt != "You are a test assistant." {
		t.Errorf("system_prompt = %q, want You are a test assistant.", cfg.LLM.SystemPrompt)
	}

	// Validate queue
	if cfg.Queue.MaxDepth != 32 {
		t.Errorf("max_depth = %d, want 32", cfg.Queue.MaxDepth)
	}

	// Validate paths
	if cfg.Paths.WorkingDir != "./sandbox/" {
		t.Errorf("working_dir = %q, want ./sandbox/", cfg.Paths.WorkingDir)
	}
	if cfg.Paths.LogDir != "./logs/" {
		t.Errorf("log_dir = %q, want ./logs/", cfg.Paths.LogDir)
	}
	if cfg.Paths.StateDir != "./state/" {
		t.Errorf("state_dir = %q, want ./state/", cfg.Paths.StateDir)
	}
	if cfg.Paths.ChannelLogDir != "" {
		t.Errorf("channel_log_dir should default to empty, got %q", cfg.Paths.ChannelLogDir)
	}

	// Validate logging
	if cfg.Logging.Level != "debug" {
		t.Errorf("level = %q, want debug", cfg.Logging.Level)
	}
	if !cfg.Logging.LogToolCalls {
		t.Error("log_tool_calls should be true")
	}
	if !cfg.Logging.LogAgentReasoning {
		t.Error("log_agent_reasoning should be true")
	}
	if !cfg.Logging.LogChannelEvents {
		t.Error("log_channel_events should be true")
	}

	// Validate bash
	if !cfg.Bash.Enabled {
		t.Error("bash enabled should be true")
	}
	if cfg.Bash.Timeout != 60*time.Second {
		t.Errorf("bash timeout should default to 60s, got %v", cfg.Bash.Timeout)
	}
	if cfg.Bash.MaxOutput != 30720 {
		t.Errorf("bash max_output should default to 30720, got %d", cfg.Bash.MaxOutput)
	}
	if len(cfg.Bash.Banned) == 0 {
		t.Error("bash banned should not be empty")
	}
	if cfg.Bash.Banned[0] != "curl" {
		t.Errorf("bash banned[0] = %q, want curl", cfg.Bash.Banned[0])
	}

	// Check new summarization defaults
	if cfg.LLM.SummarizeThreshold != 0.70 {
		t.Errorf("summarize_threshold should default to 0.70, got %f", cfg.LLM.SummarizeThreshold)
	}
	if cfg.LLM.SummarizeKeepRecent != 10 {
		t.Errorf("summarize_keep_recent should default to 10, got %d", cfg.LLM.SummarizeKeepRecent)
	}
}

func TestLoadDefaults(t *testing.T) {
	// Minimal config with only required fields
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test-model"
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check defaults
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("host should default to 127.0.0.1, got %q", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("port should default to 8080, got %d", cfg.Server.Port)
	}
	if cfg.Server.WebhookPath != "/webhook" {
		t.Errorf("webhook_path should default to /webhook, got %q", cfg.Server.WebhookPath)
	}
	if cfg.LLM.ContextTokens != 8192 {
		t.Errorf("context_tokens should default to 8192, got %d", cfg.LLM.ContextTokens)
	}
	if cfg.LLM.MaxTokens != 4096 {
		t.Errorf("max_tokens should default to 4096, got %d", cfg.LLM.MaxTokens)
	}
	if cfg.LLM.Timeout != 120*time.Second {
		t.Errorf("timeout should default to 120s, got %v", cfg.LLM.Timeout)
	}
	if cfg.LLM.MaxToolIterations != 20 {
		t.Errorf("max_tool_iterations should default to 20, got %d", cfg.LLM.MaxToolIterations)
	}
	if cfg.LLM.SystemPrompt != "You are a helpful assistant." {
		t.Errorf("system_prompt should default correctly, got %q", cfg.LLM.SystemPrompt)
	}
	if cfg.Queue.MaxDepth != 64 {
		t.Errorf("max_depth should default to 64, got %d", cfg.Queue.MaxDepth)
	}
	if cfg.Paths.WorkingDir != "./work/" {
		t.Errorf("working_dir should default to ./work/, got %q", cfg.Paths.WorkingDir)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("level should default to info, got %q", cfg.Logging.Level)
	}
	if !cfg.Logging.LogToolCalls {
		t.Error("log_tool_calls should default to true")
	}
	if !cfg.Logging.LogAgentReasoning {
		t.Error("log_agent_reasoning should default to true")
	}
	if !cfg.Logging.LogChannelEvents {
		t.Error("log_channel_events should default to true")
	}
	if cfg.Bash.Enabled != true {
		t.Errorf("bash enabled should default to true, got %v", cfg.Bash.Enabled)
	}
	if cfg.Bash.Timeout != 60*time.Second {
		t.Errorf("bash timeout should default to 60s, got %v", cfg.Bash.Timeout)
	}
	if cfg.Bash.MaxOutput != 30720 {
		t.Errorf("bash max_output should default to 30720, got %d", cfg.Bash.MaxOutput)
	}
	if len(cfg.Bash.Banned) == 0 {
		t.Error("bash banned should not be empty by default")
	}
	if cfg.Bash.Banned[0] != "curl" {
		t.Errorf("bash banned[0] = %q, want curl", cfg.Bash.Banned[0])
	}
	if cfg.Paths.ChannelLogDir != "" {
		t.Errorf("channel_log_dir should default to empty, got %q", cfg.Paths.ChannelLogDir)
	}
	if cfg.LLM.SummarizeThreshold != 0.70 {
		t.Errorf("summarize_threshold should default to 0.70, got %f", cfg.LLM.SummarizeThreshold)
	}
	if cfg.LLM.SummarizeKeepRecent != 10 {
		t.Errorf("summarize_keep_recent should default to 10, got %d", cfg.LLM.SummarizeKeepRecent)
	}
}

func TestLoadMultilineSystemPrompt(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test-model"
system_prompt = """
You are a helpful assistant.
Always think step by step.
"""
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "You are a helpful assistant.\nAlways think step by step."
	if cfg.LLM.SystemPrompt != expected {
		t.Errorf("system_prompt = %q, want %q", cfg.LLM.SystemPrompt, expected)
	}
}

func TestValidateMissingEndpoint(t *testing.T) {
	content := `
[llm]
model = "test-model"
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for missing endpoint")
	}
	if !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("expected endpoint in error, got: %v", err)
	}
}

func TestValidateMissingModel(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for missing model")
	}
	if !strings.Contains(err.Error(), "model") {
		t.Errorf("expected model in error, got: %v", err)
	}
}

func TestValidateMissingBoth(t *testing.T) {
	content := `[server]
host = 127.0.0.1
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "endpoint") || !strings.Contains(err.Error(), "model") {
		t.Errorf("expected both endpoint and model in error, got: %v", err)
	}
}

func TestValidateInvalidLogLevel(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test"

[logging]
level = invalid
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid log level")
	}
	if !strings.Contains(err.Error(), "logging.level") {
		t.Errorf("expected logging.level in error, got: %v", err)
	}
}

func TestValidateInvalidPort(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test"

[server]
port = 99999
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid port")
	}
}

func TestValidateNegativeContextTokens(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test"
context_tokens = -1
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for negative context_tokens")
	}
}

func TestValidateNegativeMaxDepth(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test"

[queue]
max_depth = -1
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for negative max_depth")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.ini")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestValidateValidConfig(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test-model"
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestStrDefault(t *testing.T) {
	data := map[string]map[string]string{
		"section": {"key": "value"},
	}

	if strDefault(data, "section", "key", "default") != "value" {
		t.Error("expected existing value")
	}
	if strDefault(data, "section", "missing", "default") != "default" {
		t.Error("expected default for missing key")
	}
	if strDefault(data, "missing", "key", "default") != "default" {
		t.Error("expected default for missing section")
	}
}

func TestIntDefault(t *testing.T) {
	data := map[string]map[string]string{
		"section": {"key": "42", "bad": "not-a-number"},
	}

	if intDefault(data, "section", "key", 0) != 42 {
		t.Error("expected 42")
	}
	if intDefault(data, "section", "bad", 10) != 10 {
		t.Error("expected default for invalid number")
	}
	if intDefault(data, "section", "missing", 10) != 10 {
		t.Error("expected default for missing key")
	}
}

func TestBoolDefault(t *testing.T) {
	data := map[string]map[string]string{
		"section": {"a": "true", "b": "false", "c": "invalid"},
	}

	if !boolDefault(data, "section", "a", false) {
		t.Error("expected true for a")
	}
	if boolDefault(data, "section", "b", true) {
		t.Error("expected false for b")
	}
	if boolDefault(data, "section", "c", true) != true {
		t.Error("expected default for invalid bool")
	}
	if boolDefault(data, "section", "missing", false) {
		t.Error("expected default for missing key")
	}
}

func TestDefaultBannedCommandsNotWrappedInString(t *testing.T) {
	if len(DefaultBannedCommands) == 0 {
		t.Fatal("DefaultBannedCommands should not be empty")
	}
	// Verify it starts and ends with valid command chars, not quotes
	if DefaultBannedCommands[0] == '"' || DefaultBannedCommands[len(DefaultBannedCommands)-1] == '"' {
		t.Error("DefaultBannedCommands should not contain quote characters")
	}
}

func TestFloatDefault(t *testing.T) {
	data := map[string]map[string]string{
		"section": {"a": "0.5", "b": "1.25", "c": "not-a-float"},
	}

	if floatDefault(data, "section", "a", 0.0) != 0.5 {
		t.Error("expected 0.5 for a")
	}
	if floatDefault(data, "section", "b", 0.0) != 1.25 {
		t.Error("expected 1.25 for b")
	}
	if floatDefault(data, "section", "c", 2.0) != 2.0 {
		t.Error("expected default for invalid float")
	}
	if floatDefault(data, "section", "missing", 3.0) != 3.0 {
		t.Error("expected default for missing key")
	}
}

func TestValidateInvalidSummarizeThreshold(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test"
summarize_threshold = 1.5
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for summarize_threshold > 1")
	}
	if !strings.Contains(err.Error(), "summarize_threshold") {
		t.Errorf("expected summarize_threshold in error, got: %v", err)
	}
}

func TestValidateNegativeSummarizeKeepRecent(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test"
summarize_keep_recent = -1
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for negative summarize_keep_recent")
	}
	if !strings.Contains(err.Error(), "summarize_keep_recent") {
		t.Errorf("expected summarize_keep_recent in error, got: %v", err)
	}
}

func TestLoadMaxBodyBytes(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test"

[server]
max_body_bytes = 2097152
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	if cfg.Server.MaxBodyBytes != 2097152 {
		t.Errorf("max_body_bytes = %d, want 2097152", cfg.Server.MaxBodyBytes)
	}

	err = cfg.Validate()
	if err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
}

func TestLoadMaxBodyBytesDefault(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test"
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	// Default should be 1MB
	if cfg.Server.MaxBodyBytes != 1048576 {
		t.Errorf("max_body_bytes should default to 1048576, got %d", cfg.Server.MaxBodyBytes)
	}
}

func TestValidateNegativeMaxBodyBytes(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test"

[server]
max_body_bytes = -1
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for negative max_body_bytes")
	}
	if !strings.Contains(err.Error(), "max_body_bytes") {
		t.Errorf("expected max_body_bytes in error, got: %v", err)
	}
}

// --- validateLLM tests ---

func TestValidateLLM(t *testing.T) {
	tests := []struct {
		name    string
		cfg     func() *Config
		wantErr []string
	}{
		{
			name: "all valid",
			cfg: func() *Config {
				return &Config{
					LLM: LLMConfig{
						Endpoint:            "http://localhost:8080/v1",
						Model:               "test-model",
						ContextTokens:       8192,
						MaxTokens:           4096,
						Timeout:             120 * time.Second,
						MaxToolIterations:   20,
						SummarizeThreshold:  0.7,
						SummarizeKeepRecent: 10,
					},
					Server:  ServerConfig{Port: 8080, MaxBodyBytes: 1048576},
					Queue:   QueueConfig{MaxDepth: 64},
					Logging: LoggingConfig{Level: "info"},
					Bash:    BashConfig{Timeout: 60 * time.Second, MaxOutput: 30720},
				}
			},
			wantErr: nil,
		},
		{
			name: "missing endpoint",
			cfg: func() *Config {
				c := validBaseConfig()
				c.LLM.Endpoint = ""
				return c
			},
			wantErr: []string{"llm.endpoint"},
		},
		{
			name: "missing model",
			cfg: func() *Config {
				c := validBaseConfig()
				c.LLM.Model = ""
				return c
			},
			wantErr: []string{"llm.model"},
		},
		{
			name: "context tokens zero",
			cfg: func() *Config {
				c := validBaseConfig()
				c.LLM.ContextTokens = 0
				return c
			},
			wantErr: []string{"context_tokens"},
		},
		{
			name: "context tokens negative",
			cfg: func() *Config {
				c := validBaseConfig()
				c.LLM.ContextTokens = -1
				return c
			},
			wantErr: []string{"context_tokens"},
		},
		{
			name: "max tokens zero",
			cfg: func() *Config {
				c := validBaseConfig()
				c.LLM.MaxTokens = 0
				return c
			},
			wantErr: []string{"max_tokens"},
		},
		{
			name: "timeout zero",
			cfg: func() *Config {
				c := validBaseConfig()
				c.LLM.Timeout = 0
				return c
			},
			wantErr: []string{"timeout"},
		},
		{
			name: "max tool iterations zero",
			cfg: func() *Config {
				c := validBaseConfig()
				c.LLM.MaxToolIterations = 0
				return c
			},
			wantErr: []string{"max_tool_iterations"},
		},
		{
			name: "summarize threshold zero",
			cfg: func() *Config {
				c := validBaseConfig()
				c.LLM.SummarizeThreshold = 0
				return c
			},
			wantErr: []string{"summarize_threshold"},
		},
		{
			name: "summarize threshold negative",
			cfg: func() *Config {
				c := validBaseConfig()
				c.LLM.SummarizeThreshold = -0.1
				return c
			},
			wantErr: []string{"summarize_threshold"},
		},
		{
			name: "summarize threshold over one",
			cfg: func() *Config {
				c := validBaseConfig()
				c.LLM.SummarizeThreshold = 1.5
				return c
			},
			wantErr: []string{"summarize_threshold"},
		},
		{
			name: "summarize keep recent negative",
			cfg: func() *Config {
				c := validBaseConfig()
				c.LLM.SummarizeKeepRecent = -1
				return c
			},
			wantErr: []string{"summarize_keep_recent"},
		},
		{
			name: "summarize keep recent zero is valid",
			cfg: func() *Config {
				c := validBaseConfig()
				c.LLM.SummarizeKeepRecent = 0
				return c
			},
			wantErr: nil,
		},
		{
			name: "summarize threshold exactly 1 is valid",
			cfg: func() *Config {
				c := validBaseConfig()
				c.LLM.SummarizeThreshold = 1
				return c
			},
			wantErr: nil,
		},
		{
			name: "multiple errors",
			cfg: func() *Config {
				c := validBaseConfig()
				c.LLM.Endpoint = ""
				c.LLM.Model = ""
				c.LLM.ContextTokens = -1
				c.LLM.MaxTokens = 0
				c.LLM.Timeout = 0
				c.LLM.MaxToolIterations = -1
				c.LLM.SummarizeThreshold = 2.0
				c.LLM.SummarizeKeepRecent = -5
				return c
			},
			wantErr: []string{"endpoint", "model", "context_tokens", "max_tokens", "timeout", "max_tool_iterations", "summarize_threshold", "summarize_keep_recent"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var errs []string
			tc.cfg().validateLLM(&errs)
			if tc.wantErr == nil {
				if len(errs) > 0 {
					t.Fatalf("expected no errors, got: %v", errs)
				}
				return
			}
			if len(errs) != len(tc.wantErr) {
				t.Fatalf("expected %d errors, got %d: %v", len(tc.wantErr), len(errs), errs)
			}
			for _, want := range tc.wantErr {
				found := false
				for _, e := range errs {
					if strings.Contains(e, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got: %v", want, errs)
				}
			}
		})
	}
}

// --- validateServer tests ---

func TestValidateServer(t *testing.T) {
	tests := []struct {
		name    string
		cfg     func() *Config
		wantErr []string
	}{
		{
			name: "all valid",
			cfg: func() *Config {
				return &Config{
					Server: ServerConfig{Port: 8080, MaxBodyBytes: 1048576},
				}
			},
			wantErr: nil,
		},
		{
			name: "port zero",
			cfg: func() *Config {
				c := validBaseConfig()
				c.Server.Port = 0
				return c
			},
			wantErr: []string{"port"},
		},
		{
			name: "port negative",
			cfg: func() *Config {
				c := validBaseConfig()
				c.Server.Port = -1
				return c
			},
			wantErr: []string{"port"},
		},
		{
			name: "port max",
			cfg: func() *Config {
				c := validBaseConfig()
				c.Server.Port = 65535
				return c
			},
			wantErr: nil,
		},
		{
			name: "port over max",
			cfg: func() *Config {
				c := validBaseConfig()
				c.Server.Port = 65536
				return c
			},
			wantErr: []string{"port"},
		},
		{
			name: "port well known",
			cfg: func() *Config {
				c := validBaseConfig()
				c.Server.Port = 80
				return c
			},
			wantErr: nil,
		},
		{
			name: "max body bytes zero",
			cfg: func() *Config {
				c := validBaseConfig()
				c.Server.MaxBodyBytes = 0
				return c
			},
			wantErr: []string{"max_body_bytes"},
		},
		{
			name: "max body bytes negative",
			cfg: func() *Config {
				c := validBaseConfig()
				c.Server.MaxBodyBytes = -1
				return c
			},
			wantErr: []string{"max_body_bytes"},
		},
		{
			name: "max body bytes one is valid",
			cfg: func() *Config {
				c := validBaseConfig()
				c.Server.MaxBodyBytes = 1
				return c
			},
			wantErr: nil,
		},
		{
			name: "multiple errors",
			cfg: func() *Config {
				c := validBaseConfig()
				c.Server.Port = 0
				c.Server.MaxBodyBytes = -1
				return c
			},
			wantErr: []string{"port", "max_body_bytes"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var errs []string
			tc.cfg().validateServer(&errs)
			if tc.wantErr == nil {
				if len(errs) > 0 {
					t.Fatalf("expected no errors, got: %v", errs)
				}
				return
			}
			if len(errs) != len(tc.wantErr) {
				t.Fatalf("expected %d errors, got %d: %v", len(tc.wantErr), len(errs), errs)
			}
			for _, want := range tc.wantErr {
				found := false
				for _, e := range errs {
					if strings.Contains(e, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got: %v", want, errs)
				}
			}
		})
	}
}

// --- validateQueue tests ---

func TestValidateQueue(t *testing.T) {
	tests := []struct {
		name    string
		cfg     func() *Config
		wantErr []string
	}{
		{
			name: "all valid",
			cfg: func() *Config {
				return &Config{Queue: QueueConfig{MaxDepth: 64}}
			},
			wantErr: nil,
		},
		{
			name: "max depth zero",
			cfg: func() *Config {
				return &Config{Queue: QueueConfig{MaxDepth: 0}}
			},
			wantErr: []string{"max_depth"},
		},
		{
			name: "max depth negative",
			cfg: func() *Config {
				return &Config{Queue: QueueConfig{MaxDepth: -1}}
			},
			wantErr: []string{"max_depth"},
		},
		{
			name: "max depth one",
			cfg: func() *Config {
				return &Config{Queue: QueueConfig{MaxDepth: 1}}
			},
			wantErr: nil,
		},
		{
			name: "max depth large",
			cfg: func() *Config {
				return &Config{Queue: QueueConfig{MaxDepth: 10000}}
			},
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var errs []string
			tc.cfg().validateQueue(&errs)
			if tc.wantErr == nil {
				if len(errs) > 0 {
					t.Fatalf("expected no errors, got: %v", errs)
				}
				return
			}
			if len(errs) != len(tc.wantErr) {
				t.Fatalf("expected %d errors, got %d: %v", len(tc.wantErr), len(errs), errs)
			}
			for _, want := range tc.wantErr {
				found := false
				for _, e := range errs {
					if strings.Contains(e, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got: %v", want, errs)
				}
			}
		})
	}
}

// --- validateLogging tests ---

func TestValidateLogging(t *testing.T) {
	tests := []struct {
		name    string
		cfg     func() *Config
		wantErr []string
	}{
		{
			name: "debug",
			cfg: func() *Config {
				return &Config{Logging: LoggingConfig{Level: "debug"}}
			},
			wantErr: nil,
		},
		{
			name: "info",
			cfg: func() *Config {
				return &Config{Logging: LoggingConfig{Level: "info"}}
			},
			wantErr: nil,
		},
		{
			name: "warn",
			cfg: func() *Config {
				return &Config{Logging: LoggingConfig{Level: "warn"}}
			},
			wantErr: nil,
		},
		{
			name: "error",
			cfg: func() *Config {
				return &Config{Logging: LoggingConfig{Level: "error"}}
			},
			wantErr: nil,
		},
		{
			name: "uppercase DEBUG",
			cfg: func() *Config {
				return &Config{Logging: LoggingConfig{Level: "DEBUG"}}
			},
			wantErr: nil,
		},
		{
			name: "mixed case Info",
			cfg: func() *Config {
				return &Config{Logging: LoggingConfig{Level: "Info"}}
			},
			wantErr: nil,
		},
		{
			name: "uppercase WARN",
			cfg: func() *Config {
				return &Config{Logging: LoggingConfig{Level: "WARN"}}
			},
			wantErr: nil,
		},
		{
			name: "uppercase ERROR",
			cfg: func() *Config {
				return &Config{Logging: LoggingConfig{Level: "ERROR"}}
			},
			wantErr: nil,
		},
		{
			name: "invalid level",
			cfg: func() *Config {
				return &Config{Logging: LoggingConfig{Level: "invalid"}}
			},
			wantErr: []string{"logging.level"},
		},
		{
			name: "empty level",
			cfg: func() *Config {
				return &Config{Logging: LoggingConfig{Level: ""}}
			},
			wantErr: []string{"logging.level"},
		},
		{
			name: "random string level",
			cfg: func() *Config {
				return &Config{Logging: LoggingConfig{Level: "foobar"}}
			},
			wantErr: []string{"logging.level"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var errs []string
			tc.cfg().validateLogging(&errs)
			if tc.wantErr == nil {
				if len(errs) > 0 {
					t.Fatalf("expected no errors, got: %v", errs)
				}
				return
			}
			if len(errs) != len(tc.wantErr) {
				t.Fatalf("expected %d errors, got %d: %v", len(tc.wantErr), len(errs), errs)
			}
			for _, want := range tc.wantErr {
				found := false
				for _, e := range errs {
					if strings.Contains(e, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got: %v", want, errs)
				}
			}
		})
	}
}

// --- validateBash tests ---

func TestValidateBash(t *testing.T) {
	tests := []struct {
		name    string
		cfg     func() *Config
		wantErr []string
	}{
		{
			name: "all valid",
			cfg: func() *Config {
				return &Config{
					Bash: BashConfig{
						Timeout:   60 * time.Second,
						MaxOutput: 30720,
					},
				}
			},
			wantErr: nil,
		},
		{
			name: "timeout zero",
			cfg: func() *Config {
				return &Config{
					Bash: BashConfig{
						Timeout:   0,
						MaxOutput: 30720,
					},
				}
			},
			wantErr: []string{"bash.timeout"},
		},
		{
			name: "timeout negative",
			cfg: func() *Config {
				return &Config{
					Bash: BashConfig{
						Timeout:   -10 * time.Second,
						MaxOutput: 30720,
					},
				}
			},
			wantErr: []string{"bash.timeout"},
		},
		{
			name: "timeout one second",
			cfg: func() *Config {
				return &Config{
					Bash: BashConfig{
						Timeout:   1 * time.Second,
						MaxOutput: 30720,
					},
				}
			},
			wantErr: nil,
		},
		{
			name: "max output zero",
			cfg: func() *Config {
				return &Config{
					Bash: BashConfig{
						Timeout:   60 * time.Second,
						MaxOutput: 0,
					},
				}
			},
			wantErr: []string{"max_output"},
		},
		{
			name: "max output negative",
			cfg: func() *Config {
				return &Config{
					Bash: BashConfig{
						Timeout:   60 * time.Second,
						MaxOutput: -1,
					},
				}
			},
			wantErr: []string{"max_output"},
		},
		{
			name: "max output one",
			cfg: func() *Config {
				return &Config{
					Bash: BashConfig{
						Timeout:   60 * time.Second,
						MaxOutput: 1,
					},
				}
			},
			wantErr: nil,
		},
		{
			name: "max output large",
			cfg: func() *Config {
				return &Config{
					Bash: BashConfig{
						Timeout:   60 * time.Second,
						MaxOutput: 1048576,
					},
				}
			},
			wantErr: nil,
		},
		{
			name: "multiple errors",
			cfg: func() *Config {
				return &Config{
					Bash: BashConfig{
						Timeout:   0,
						MaxOutput: -1,
					},
				}
			},
			wantErr: []string{"bash.timeout", "max_output"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var errs []string
			tc.cfg().validateBash(&errs)
			if tc.wantErr == nil {
				if len(errs) > 0 {
					t.Fatalf("expected no errors, got: %v", errs)
				}
				return
			}
			if len(errs) != len(tc.wantErr) {
				t.Fatalf("expected %d errors, got %d: %v", len(tc.wantErr), len(errs), errs)
			}
			for _, want := range tc.wantErr {
				found := false
				for _, e := range errs {
					if strings.Contains(e, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got: %v", want, errs)
				}
			}
		})
	}
}

// validBaseConfig returns a Config that passes all validation checks.
func validBaseConfig() *Config {
	return &Config{
		LLM: LLMConfig{
			Endpoint:            "http://localhost:8080/v1",
			Model:               "test-model",
			ContextTokens:       8192,
			MaxTokens:           4096,
			Timeout:             120 * time.Second,
			MaxToolIterations:   20,
			SummarizeThreshold:  0.7,
			SummarizeKeepRecent: 10,
		},
		Server:  ServerConfig{Port: 8080, MaxBodyBytes: 1048576},
		Queue:   QueueConfig{MaxDepth: 64},
		Logging: LoggingConfig{Level: "info"},
		Bash:    BashConfig{Timeout: 60 * time.Second, MaxOutput: 30720},
	}
}

func TestStrListDefault(t *testing.T) {
	tests := []struct {
		name       string
		data       map[string]map[string]string
		section    string
		key        string
		defaultVal string
		want       []string
	}{
		{
			name:       "empty default returns empty slice",
			data:       nil,
			section:    "tools.bash",
			key:        "banned",
			defaultVal: "",
			want:       nil,
		},
		{
			name:       "single item",
			data:       map[string]map[string]string{"s": {"k": "foo"}},
			section:    "s",
			key:        "k",
			defaultVal: "bar",
			want:       []string{"foo"},
		},
		{
			name:       "comma-separated with whitespace trimmed and lowercased",
			data:       map[string]map[string]string{"s": {"k": " Foo , Bar , BAZ "}},
			section:    "s",
			key:        "k",
			defaultVal: "",
			want:       []string{"foo", "bar", "baz"},
		},
		{
			name:       "mixed case lowercased",
			data:       map[string]map[string]string{"s": {"k": "ABC"}},
			section:    "s",
			key:        "k",
			defaultVal: "",
			want:       []string{"abc"},
		},
		{
			name:       "empty entries filtered out",
			data:       map[string]map[string]string{"s": {"k": "foo,,bar"}},
			section:    "s",
			key:        "k",
			defaultVal: "",
			want:       []string{"foo", "bar"},
		},
		{
			name:       "missing section falls back to split default",
			data:       nil,
			section:    "tools.bash",
			key:        "banned",
			defaultVal: "curl,wget",
			want:       []string{"curl", "wget"},
		},
		{
			name:       "missing key falls back to split default",
			data:       map[string]map[string]string{"s": {"other": "value"}},
			section:    "s",
			key:        "k",
			defaultVal: "a,b,c",
			want:       []string{"a", "b", "c"},
		},
		{
			name:       "default with empty entries filtered",
			data:       nil,
			section:    "s",
			key:        "k",
			defaultVal: "foo,,bar,,",
			want:       []string{"foo", "bar"},
		},
		{
			name:       "default with whitespace in entries trimmed and lowercased",
			data:       nil,
			section:    "s",
			key:        "k",
			defaultVal: " FoO ,  BAr  ",
			want:       []string{"foo", "bar"},
		},
		{
			name:       "nil section with empty default returns nil",
			data:       nil,
			section:    "s",
			key:        "k",
			defaultVal: "",
			want:       nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strListDefault(tt.data, tt.section, tt.key, tt.defaultVal)
			if len(got) == 0 && len(tt.want) == 0 {
				if (got == nil) != (tt.want == nil) {
					t.Errorf("got %v (%v), want %v (%v)", got, got == nil, tt.want, tt.want == nil)
				}
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
