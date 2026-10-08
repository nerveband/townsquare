// Package wa wraps whatsmeow for Townsquare: a linked device that only sends.
// It never downloads chat history and never stores message bodies.
package wa

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"google.golang.org/protobuf/proto"
	_ "modernc.org/sqlite"
)

// DeviceName is what shows up under Linked devices on the phone.
const DeviceName = "Townsquare"

func init() {
	store.DeviceProps.Os = proto.String(DeviceName)
	store.DeviceProps.RequireFullSync = proto.Bool(false)
	// Ask the phone for as little history as possible.
	hs := store.DeviceProps.HistorySyncConfig
	hs.FullSyncDaysLimit = proto.Uint32(1)
	hs.FullSyncSizeMbLimit = proto.Uint32(1)
	hs.RecentSyncDaysLimit = proto.Uint32(1)
	hs.StorageQuotaMb = proto.Uint32(10)
	hs.InitialSyncMaxMessagesPerChat = proto.Uint32(1)
	hs.ThumbnailSyncDaysLimit = proto.Uint32(0)
}

// Open opens (or creates) the session store in dataDir and returns a client.
// The caller is responsible for Connect/Disconnect.
func Open(ctx context.Context, dataDir, logLevel string) (*whatsmeow.Client, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.Join(dataDir, "session.db") + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	container, err := sqlstore.New(ctx, "sqlite", dsn, Logger("Store", logLevel))
	if err != nil {
		return nil, fmt.Errorf("open session store: %w", err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("load device: %w", err)
	}
	cli := whatsmeow.NewClient(device, Logger("WA", logLevel))
	// Do not download history blobs. Still acknowledge them so the phone stops retrying.
	cli.ManualHistorySyncDownload = true
	cli.DisableManualHistorySyncReceipt = false
	return cli, nil
}

// ConnectPaired connects an already-paired client and waits until it is ready.
func ConnectPaired(ctx context.Context, cli *whatsmeow.Client) error {
	if cli.Store.ID == nil {
		return fmt.Errorf("not paired yet: run `townsquare pair` first")
	}
	if err := cli.ConnectContext(ctx); err != nil {
		return err
	}
	if !cli.WaitForConnection(30_000_000_000) {
		return fmt.Errorf("timed out waiting for WhatsApp connection")
	}
	return nil
}
