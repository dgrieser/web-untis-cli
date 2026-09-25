package keyring

import (
	"errors"
	"os"
	"testing"
)

// TestSystemRealKeyring round-trips through the operating system keyring. It is
// opt-in because CI has no Secret Service and the test touches the user's store.
func TestSystemRealKeyring(t *testing.T) {
	if os.Getenv("WEBUNTIS_CLI_KEYRING_TEST") == "" {
		t.Skip("set WEBUNTIS_CLI_KEYRING_TEST=1 to test against the real keyring")
	}
	const account = "webuntis-cli-keyring-test"
	var store System
	t.Cleanup(func() { _ = store.Delete(account) })

	if err := store.Set(account, "first"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Set(account, "second"); err != nil {
		t.Fatalf("Set again: %v", err)
	}
	if got, err := store.Get(account); err != nil || got != "second" {
		t.Fatalf("Get = %q, %v; want the replaced value", got, err)
	}
	if err := store.Delete(account); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(account); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
}
