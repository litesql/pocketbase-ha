package config

import (
	"strings"
	"testing"
	"time"
)

func TestParseDefaults(t *testing.T) {
	cfg, err := Parse(func(string) string { return "" })
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if cfg.ReplicationStream != DefaultReplicationStream {
		t.Fatalf("ReplicationStream = %q, want %q", cfg.ReplicationStream, DefaultReplicationStream)
	}
	if cfg.StreamMaxAge != DefaultStreamMaxAge {
		t.Fatalf("StreamMaxAge = %s, want %s", cfg.StreamMaxAge, DefaultStreamMaxAge)
	}
	if cfg.AsyncPublisher {
		t.Fatal("AsyncPublisher unexpectedly true")
	}
	if cfg.EmbeddedNATS != nil {
		t.Fatalf("EmbeddedNATS = %+v, want nil", cfg.EmbeddedNATS)
	}
	if cfg.Replicas != nil {
		t.Fatalf("Replicas = %v, want nil", cfg.Replicas)
	}
	if cfg.GRPCPort != nil {
		t.Fatalf("GRPCPort = %v, want nil", cfg.GRPCPort)
	}
}

func TestParseAllEnv(t *testing.T) {
	env := map[string]string{
		"PB_NAME":                "node1",
		"PB_REPLICATION_URL":     "nats://localhost:4222",
		"PB_ASYNC_PUBLISHER":     "true",
		"PB_ASYNC_PUBLISHER_DIR": "/tmp/outbox",
		"PB_REPLICATION_STREAM":  "custom",
		"PB_NATS_PORT":           "4222",
		"PB_NATS_STORE_DIR":      "/tmp/nats",
		"PB_REPLICAS":            "1",
		"PB_ROW_IDENTIFY":        "rowid",
		"PB_STATIC_LEADER":       "http://leader:8090",
		"PB_LOCAL_TARGET":        "http://node1:8090",
		"PB_GRPC_PORT":           "9090",
		"PB_GRPC_TOKEN":          "secret",
		"PB_STREAM_MAX_AGE":      "1h",
		"PB_SUPERUSER_EMAIL":     "test@example.com",
		"PB_SUPERUSER_PASS":      "password",
	}

	cfg, err := Parse(mapGetenv(env))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if cfg.Name != "node1" {
		t.Fatalf("Name = %q", cfg.Name)
	}
	if cfg.ReplicationURL != "nats://localhost:4222" {
		t.Fatalf("ReplicationURL = %q", cfg.ReplicationURL)
	}
	if !cfg.AsyncPublisher {
		t.Fatal("AsyncPublisher = false")
	}
	if cfg.AsyncPublisherDir != "/tmp/outbox" {
		t.Fatalf("AsyncPublisherDir = %q", cfg.AsyncPublisherDir)
	}
	if cfg.ReplicationStream != "custom" {
		t.Fatalf("ReplicationStream = %q", cfg.ReplicationStream)
	}
	if cfg.EmbeddedNATS == nil || cfg.EmbeddedNATS.Port != 4222 || cfg.EmbeddedNATS.StoreDir != "/tmp/nats" {
		t.Fatalf("EmbeddedNATS = %+v", cfg.EmbeddedNATS)
	}
	if cfg.Replicas == nil || *cfg.Replicas != 1 {
		t.Fatalf("Replicas = %v", cfg.Replicas)
	}
	if cfg.RowIdentify != RowIdentifyRowid {
		t.Fatalf("RowIdentify = %q", cfg.RowIdentify)
	}
	if cfg.StaticLeader != "http://leader:8090" {
		t.Fatalf("StaticLeader = %q", cfg.StaticLeader)
	}
	if cfg.LocalTarget != "http://node1:8090" {
		t.Fatalf("LocalTarget = %q", cfg.LocalTarget)
	}
	if cfg.GRPCPort == nil || *cfg.GRPCPort != 9090 {
		t.Fatalf("GRPCPort = %v", cfg.GRPCPort)
	}
	if cfg.GRPCToken != "secret" {
		t.Fatalf("GRPCToken = %q", cfg.GRPCToken)
	}
	if cfg.StreamMaxAge != time.Hour {
		t.Fatalf("StreamMaxAge = %s", cfg.StreamMaxAge)
	}
	if cfg.SuperuserEmail != "test@example.com" || cfg.SuperuserPass != "password" {
		t.Fatalf("superuser = %q / %q", cfg.SuperuserEmail, cfg.SuperuserPass)
	}
}

func TestParseNATSConfigFileOverridesPort(t *testing.T) {
	cfg, err := Parse(mapGetenv(map[string]string{
		"PB_NATS_CONFIG":    "/etc/nats/node1.cfg",
		"PB_NATS_PORT":      "4222",
		"PB_NATS_STORE_DIR": "/tmp/nats",
	}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.EmbeddedNATS == nil || cfg.EmbeddedNATS.File != "/etc/nats/node1.cfg" {
		t.Fatalf("EmbeddedNATS = %+v", cfg.EmbeddedNATS)
	}
	if cfg.EmbeddedNATS.Port != 0 {
		t.Fatalf("Port = %d, want 0 when config file is set", cfg.EmbeddedNATS.Port)
	}
}

func TestParseInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		env    map[string]string
		errSub string
	}{
		{"async publisher", map[string]string{"PB_ASYNC_PUBLISHER": "yes-please"}, "invalid PB_ASYNC_PUBLISHER"},
		{"nats port", map[string]string{"PB_NATS_PORT": "abc"}, "invalid PB_NATS_PORT"},
		{"replicas", map[string]string{"PB_REPLICAS": "x"}, "invalid PB_REPLICAS"},
		{"row identify", map[string]string{"PB_ROW_IDENTIFY": "guid"}, "invalid PB_ROW_IDENTIFY"},
		{"grpc port", map[string]string{"PB_GRPC_PORT": "nope"}, "invalid PB_GRPC_PORT"},
		{"stream max age", map[string]string{"PB_STREAM_MAX_AGE": "forever"}, "invalid PB_STREAM_MAX_AGE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(mapGetenv(tt.env))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.errSub) {
				t.Fatalf("error %q does not contain %q", err, tt.errSub)
			}
		})
	}
}

func TestParseRowIdentifyValues(t *testing.T) {
	for _, value := range []string{RowIdentifyPK, RowIdentifyRowid, RowIdentifyFull} {
		cfg, err := Parse(mapGetenv(map[string]string{"PB_ROW_IDENTIFY": value}))
		if err != nil {
			t.Fatalf("%s: %v", value, err)
		}
		if cfg.RowIdentify != value {
			t.Fatalf("RowIdentify = %q, want %q", cfg.RowIdentify, value)
		}
	}
}

func mapGetenv(env map[string]string) func(string) string {
	return func(key string) string {
		return env[key]
	}
}
