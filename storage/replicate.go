package storage

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/hanzoai/replicate"
	"github.com/luxfi/age"
)

// StartReplicate streams SQLite WAL changes to S3 with PQ encryption.
// Returns a stop function. If REPLICATE_S3_ENDPOINT is unset, returns a noop.
func StartReplicate(dbPath string) func() {
	endpoint := os.Getenv("REPLICATE_S3_ENDPOINT")
	if endpoint == "" {
		return func() {}
	}

	bucket := envOr("REPLICATE_S3_BUCKET", "replicate")
	prefix := os.Getenv("REPLICATE_S3_PATH")
	if prefix == "" {
		prefix, _ = os.Hostname()
	}
	region := envOr("REPLICATE_S3_REGION", "us-central1")

	replicaURL := fmt.Sprintf("s3://%s/%s?endpoint=%s&region=%s&force-path-style=true",
		url.PathEscape(bucket),
		url.PathEscape(prefix),
		url.QueryEscape(endpoint),
		url.QueryEscape(region),
	)
	if ak := os.Getenv("REPLICATE_S3_ACCESS_KEY"); ak != "" {
		replicaURL += "&access_key=" + url.QueryEscape(ak)
	}
	if sk := os.Getenv("REPLICATE_S3_SECRET_KEY"); sk != "" {
		replicaURL += "&secret_key=" + url.QueryEscape(sk)
	}

	client, err := replicate.NewReplicaClientFromURL(replicaURL)
	if err != nil {
		log.Printf("[replicate] invalid config: %v", err)
		return func() {}
	}

	db := replicate.NewDB(dbPath)
	replica := replicate.NewReplicaWithClient(db, client)

	if v := os.Getenv("REPLICATE_SYNC_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			replica.SyncInterval = d
		}
	}

	// PQ encryption: ML-KEM-768 + X25519 hybrid via luxfi/age
	if recipientStr := os.Getenv("REPLICATE_AGE_RECIPIENT"); recipientStr != "" {
		if rcs, err := age.ParseRecipients(strings.NewReader(recipientStr)); err == nil {
			replica.AgeRecipients = rcs
		}
	}
	if identityStr := os.Getenv("REPLICATE_AGE_IDENTITY"); identityStr != "" {
		if ids, err := age.ParseIdentities(strings.NewReader(identityStr)); err == nil {
			replica.AgeIdentities = ids
		}
	}

	db.Replica = replica

	if err := db.Open(); err != nil {
		log.Printf("[replicate] failed: %v", err)
		return func() {}
	}

	log.Printf("[replicate] streaming %s → s3://%s/%s (pq=%v)",
		dbPath, bucket, prefix, len(replica.AgeRecipients) > 0)

	return func() { _ = db.Close(context.Background()) }
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
