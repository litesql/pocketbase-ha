package hadriver

import (
	"testing"

	"github.com/litesql/pocketbase-ha/internal/config"
)

func TestOptionsAlwaysIncludesCoreSettings(t *testing.T) {
	replicas := 1
	grpcPort := 9090
	cfg := config.Config{
		Name:              "node1",
		ReplicationURL:    "nats://localhost:4222",
		AsyncPublisher:    true,
		AsyncPublisherDir: "/tmp/outbox",
		ReplicationStream: "pb",
		EmbeddedNATS:      &config.EmbeddedNATS{Port: 4222, StoreDir: "/tmp/nats"},
		Replicas:          &replicas,
		RowIdentify:       config.RowIdentifyPK,
		StaticLeader:      "http://leader:8090",
		LocalTarget:       "http://node1:8090",
		GRPCPort:          &grpcPort,
		GRPCToken:         "secret",
		StreamMaxAge:      config.DefaultStreamMaxAge,
	}

	bootstrap := make(chan struct{})
	opts := Options(cfg, nil, bootstrap)
	if len(opts) == 0 {
		t.Fatal("expected driver options")
	}
}
