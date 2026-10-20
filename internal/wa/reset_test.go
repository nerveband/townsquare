package wa

import (
	"context"
	"os"
	"testing"

	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

func TestResetRemovedDevice(t *testing.T) {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "ts-wa-reset-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	cli, err := Open(ctx, dir, "ERROR")
	if err != nil {
		t.Fatal(err)
	}
	if reset, err := ResetRemoved(cli); reset || err != nil {
		t.Fatalf("fresh device must not reset: %v %v", reset, err)
	}
	// Pretend the phone linked us, then removed the device.
	jid := types.JID{User: "15550000000", Device: 3, Server: types.DefaultUserServer}
	cli.Store.ID = &jid
	cli.Store.Account = &waAdv.ADVSignedDeviceIdentity{Details: []byte{1}, AccountSignature: make([]byte, 64), AccountSignatureKey: make([]byte, 32), DeviceSignature: make([]byte, 64)}
	if err := cli.Store.Save(ctx); err != nil {
		t.Fatal(err)
	}
	if err := cli.Store.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if err := cli.Store.Save(ctx); err != store.ErrDeviceDeleted {
		t.Fatalf("a removed device must be unusable, got %v", err)
	}
	if cli.Store.ID != nil {
		t.Fatal("removed device still has an id")
	}

	// Linking must start clean: a fresh, unlinked, usable device.
	// (A cancelled context keeps the test off the network.)
	dead, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := StartLink(dead, cli, "", true); err != nil {
		t.Fatalf("link after removal: %v", err)
	}
	if cli.Store.Deleted || cli.Store.ID != nil || cli.Store.NoiseKey == nil {
		t.Fatalf("device not reset: deleted=%v", cli.Store.Deleted)
	}
	if reset, _ := ResetRemoved(cli); reset {
		t.Fatal("second reset should be a no-op")
	}

	// Nothing of the old device survives a restart.
	cli.Disconnect()
	again, err := Open(ctx, dir, "ERROR")
	if err != nil {
		t.Fatal(err)
	}
	if again.Store.ID != nil {
		t.Fatal("old device came back after reopening")
	}
}
