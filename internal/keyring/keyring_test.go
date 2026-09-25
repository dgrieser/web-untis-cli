package keyring

import (
	"errors"
	"testing"

	gokeyring "github.com/zalando/go-keyring"
)

// useMock swaps go-keyring for an in-memory store; no test may touch the
// user's keyring.
func useMock(t *testing.T) {
	t.Helper()
	gokeyring.MockInit()
	t.Cleanup(gokeyring.MockInit)
}

func TestSystemRoundTrip(t *testing.T) {
	useMock(t)
	var store System
	if err := store.Set("default", "secret"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, err := store.Get("default"); err != nil || got != "secret" {
		t.Fatalf("Get = %q, %v; want secret", got, err)
	}
	if got, err := gokeyring.Get(Service, "default"); err != nil || got != "secret" {
		t.Fatalf("stored under service %q: %q, %v", Service, got, err)
	}
	if err := store.Delete("default"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get("default"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := store.Delete("default"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete = %v, want ErrNotFound", err)
	}
}

func TestSystemSeparateServices(t *testing.T) {
	useMock(t)
	web := System{}
	smtp := System{ServiceName: Service + "-smtp"}
	if err := web.Set("default", "a"); err != nil {
		t.Fatal(err)
	}
	if err := smtp.Set("default", "b"); err != nil {
		t.Fatal(err)
	}
	if err := smtp.Delete("default"); err != nil {
		t.Fatal(err)
	}
	if got, err := web.Get("default"); err != nil || got != "a" {
		t.Fatalf("web password = %q, %v", got, err)
	}
}

func TestSystemTreatsEmptyValueAsNotFound(t *testing.T) {
	useMock(t)
	if err := gokeyring.Set(Service, "default", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := (System{}).Get("default"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get = %v, want ErrNotFound", err)
	}
}

func TestSystemRefusesEmptySecret(t *testing.T) {
	useMock(t)
	if err := (System{}).Set("default", ""); err == nil {
		t.Fatal("Set(\"\") should fail")
	}
}
