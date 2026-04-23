package consumer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestKafkaReaderConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     KafkaReaderConfig
		wantErr string
	}{
		{
			name:    "missing brokers",
			cfg:     KafkaReaderConfig{Topic: "t", GroupID: "g"},
			wantErr: "brokers",
		},
		{
			name:    "missing topic",
			cfg:     KafkaReaderConfig{Brokers: []string{"b:9092"}, GroupID: "g"},
			wantErr: "topic",
		},
		{
			name:    "missing group id",
			cfg:     KafkaReaderConfig{Brokers: []string{"b:9092"}, Topic: "t"},
			wantErr: "group id",
		},
		{
			name:    "bad start offset",
			cfg:     KafkaReaderConfig{Brokers: []string{"b:9092"}, Topic: "t", GroupID: "g", StartOffset: "middle"},
			wantErr: "start offset",
		},
		{
			name:    "valid minimal",
			cfg:     KafkaReaderConfig{Brokers: []string{"b:9092"}, Topic: "t", GroupID: "g"},
			wantErr: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestKafkaReaderConfigApplyDefaults(t *testing.T) {
	cfg := KafkaReaderConfig{}
	cfg.ApplyDefaults()
	if cfg.ClientID == "" {
		t.Fatal("client id should default")
	}
	if cfg.StartOffset != "first" {
		t.Fatalf("start offset default = %q, want first", cfg.StartOffset)
	}
	if cfg.MaxBytes == 0 || cfg.MinBytes == 0 || cfg.MaxWait == 0 {
		t.Fatalf("min/max bytes and max wait should default, got %+v", cfg)
	}
}

func TestCockroachConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     CockroachConfig
		wantErr string
	}{
		{"missing dsn", CockroachConfig{}, "dsn"},
		{"blank dsn", CockroachConfig{DSN: "   "}, "dsn"},
		{"negative open", CockroachConfig{DSN: "x", MaxOpenConns: -1}, "max open"},
		{"negative idle", CockroachConfig{DSN: "x", MaxIdleConns: -1}, "max idle"},
		{"valid", CockroachConfig{DSN: "postgresql://host/db"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestCockroachConfigApplyDefaults(t *testing.T) {
	cfg := CockroachConfig{DSN: "x"}
	cfg.ApplyDefaults()
	if cfg.MaxOpenConns == 0 || cfg.MaxIdleConns == 0 {
		t.Fatalf("conn counts should default, got %+v", cfg)
	}
	if cfg.ConnMaxLifetime == 0 || cfg.ConnMaxLifetime > 24*time.Hour {
		t.Fatalf("conn max lifetime default unreasonable: %v", cfg.ConnMaxLifetime)
	}
}

func TestConfigValidateComposes(t *testing.T) {
	cfg := &Config{}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "kafka") {
		t.Fatalf("expected kafka error, got %v", err)
	}
	cfg.Kafka = KafkaReaderConfig{Brokers: []string{"b:9092"}, Topic: "t", GroupID: "g"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "cockroach") {
		t.Fatalf("expected cockroach error, got %v", err)
	}
	cfg.Cockroach = CockroachConfig{DSN: "postgresql://host/db"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	body := `{
      "Kafka": {"Brokers":["b:9092"], "Topic":"t", "GroupID":"g"},
      "Cockroach": {"DSN":"postgresql://host/db", "MaxOpenConns": 4}
    }`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	require.Equal(t, []string{"b:9092"}, cfg.Kafka.Brokers)
	require.Equal(t, "t", cfg.Kafka.Topic)
	require.Equal(t, "postgresql://host/db", cfg.Cockroach.DSN)
	require.Equal(t, 4, cfg.Cockroach.MaxOpenConns)
}

func TestLoadConfigRejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"Kafka":{}}`), 0o600))

	_, err := LoadConfig(path)
	require.Error(t, err)
}

func TestLoadConfigMissingFile(t *testing.T) {
	_, err := LoadConfig(filepath.Join(t.TempDir(), "nope.json"))
	require.Error(t, err)
}
