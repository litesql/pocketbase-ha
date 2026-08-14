package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	DefaultReplicationStream = "pb"
	DefaultStreamMaxAge      = 72 * time.Hour

	envName              = "PB_NAME"
	envReplicationURL    = "PB_REPLICATION_URL"
	envAsyncPublisher    = "PB_ASYNC_PUBLISHER"
	envAsyncPublisherDir = "PB_ASYNC_PUBLISHER_DIR"
	envReplicationStream = "PB_REPLICATION_STREAM"
	envNATSConfig        = "PB_NATS_CONFIG"
	envNATSPort          = "PB_NATS_PORT"
	envNATSStoreDir      = "PB_NATS_STORE_DIR"
	envReplicas          = "PB_REPLICAS"
	envRowIdentify       = "PB_ROW_IDENTIFY"
	envStaticLeader      = "PB_STATIC_LEADER"
	envLocalTarget       = "PB_LOCAL_TARGET"
	envGRPCPort          = "PB_GRPC_PORT"
	envGRPCToken         = "PB_GRPC_TOKEN"
	envStreamMaxAge      = "PB_STREAM_MAX_AGE"
	envSuperuserEmail    = "PB_SUPERUSER_EMAIL"
	envSuperuserPass     = "PB_SUPERUSER_PASS"
	envFileCacheDir      = "PB_FILECACHE_DIR"
	envFileCacheSize     = "PB_FILECACHE_SIZE_BYTES"
)

// Row identify strategies accepted by PB_ROW_IDENTIFY.
const (
	RowIdentifyPK    = "pk"
	RowIdentifyRowid = "rowid"
	RowIdentifyFull  = "full"
)

// Config is the typed cluster configuration sourced from PB_* environment variables.
type Config struct {
	Name               string
	ReplicationURL     string
	AsyncPublisher     bool
	AsyncPublisherDir  string
	ReplicationStream  string
	EmbeddedNATS       *EmbeddedNATS
	Replicas           *int
	RowIdentify        string
	StaticLeader       string
	LocalTarget        string
	GRPCPort           *int
	GRPCToken          string
	StreamMaxAge       time.Duration
	SuperuserEmail     string
	SuperuserPass      string
	FileCacheDir       string
	FileCacheSizeBytes int64
}

// EmbeddedNATS holds embedded NATS server settings. File, when set, overrides Port/StoreDir.
type EmbeddedNATS struct {
	File     string
	Port     int
	StoreDir string
}

// FromEnv loads Config from the process environment.
func FromEnv() (Config, error) {
	return Parse(os.Getenv)
}

// Parse loads Config using getenv. Empty values keep defaults.
func Parse(getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	cfg := Config{
		Name:              getenv(envName),
		ReplicationURL:    getenv(envReplicationURL),
		AsyncPublisherDir: getenv(envAsyncPublisherDir),
		StaticLeader:      getenv(envStaticLeader),
		LocalTarget:       getenv(envLocalTarget),
		GRPCToken:         getenv(envGRPCToken),
		SuperuserEmail:    getenv(envSuperuserEmail),
		SuperuserPass:     getenv(envSuperuserPass),
		ReplicationStream: getenv(envReplicationStream),
		StreamMaxAge:      DefaultStreamMaxAge,
		FileCacheDir:      getenv(envFileCacheDir),
	}

	if cfg.ReplicationStream == "" {
		cfg.ReplicationStream = DefaultReplicationStream
	}

	if async := getenv(envAsyncPublisher); async != "" {
		b, err := strconv.ParseBool(async)
		if err != nil {
			return Config{}, fmt.Errorf("invalid %s: %w", envAsyncPublisher, err)
		}
		cfg.AsyncPublisher = b
	}

	if natsConfigFile := getenv(envNATSConfig); natsConfigFile != "" {
		cfg.EmbeddedNATS = &EmbeddedNATS{File: natsConfigFile}
	} else if natsPort := getenv(envNATSPort); natsPort != "" {
		port, err := strconv.Atoi(natsPort)
		if err != nil {
			return Config{}, fmt.Errorf("invalid %s value: %w", envNATSPort, err)
		}
		cfg.EmbeddedNATS = &EmbeddedNATS{
			Port:     port,
			StoreDir: getenv(envNATSStoreDir),
		}
	}

	if replicas := getenv(envReplicas); replicas != "" {
		n, err := strconv.Atoi(replicas)
		if err != nil {
			return Config{}, fmt.Errorf("invalid %s value: %w", envReplicas, err)
		}
		cfg.Replicas = &n
	}

	if rowIdentify := getenv(envRowIdentify); rowIdentify != "" {
		switch rowIdentify {
		case RowIdentifyPK, RowIdentifyRowid, RowIdentifyFull:
			cfg.RowIdentify = rowIdentify
		default:
			return Config{}, fmt.Errorf("invalid %s: %s", envRowIdentify, rowIdentify)
		}
	}

	if grpcPort := getenv(envGRPCPort); grpcPort != "" {
		port, err := strconv.Atoi(grpcPort)
		if err != nil {
			return Config{}, fmt.Errorf("invalid %s value: %w", envGRPCPort, err)
		}
		cfg.GRPCPort = &port
	}

	if localHistoryMaxAge := getenv(envStreamMaxAge); localHistoryMaxAge != "" {
		maxAge, err := time.ParseDuration(localHistoryMaxAge)
		if err != nil {
			return Config{}, fmt.Errorf("invalid %s value: %w", envStreamMaxAge, err)
		}
		cfg.StreamMaxAge = maxAge
	}

	if size := getenv(envFileCacheSize); size != "" {
		n, err := strconv.ParseInt(size, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("invalid %s value: %w", envFileCacheSize, err)
		}
		if n < 0 {
			return Config{}, fmt.Errorf("invalid %s value: must be >= 0", envFileCacheSize)
		}
		cfg.FileCacheSizeBytes = n
	}

	return cfg, nil
}
