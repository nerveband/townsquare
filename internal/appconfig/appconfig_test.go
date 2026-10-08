package appconfig

import "testing"

func TestConfigAndLock(t *testing.T) {
	dir := t.TempDir()
	if c, err := Load(dir); err != nil || c.ListenAddr() != DefaultListen {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	if err := Save(dir, Config{Listen: "nope"}); err == nil {
		t.Fatal("bad listen accepted")
	}
	if err := Save(dir, Config{Tailscale: "Bad Name"}); err == nil {
		t.Fatal("bad tailscale name accepted")
	}
	if err := Save(dir, Config{Listen: "0.0.0.0:8890", Tailscale: "townsquare"}); err != nil {
		t.Fatal(err)
	}
	if c, _ := Load(dir); c.ListenAddr() != "0.0.0.0:8890" || c.Tailscale != "townsquare" {
		t.Fatalf("loaded %+v", c)
	}
	ok, err := Lock(dir)
	if !ok || err != nil {
		t.Fatalf("first lock: %v %v", ok, err)
	}
}
