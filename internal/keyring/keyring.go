// Package keyring stores credentials in the operating system keyring:
// Secret Service on Linux, Keychain on macOS, Credential Manager on Windows.
package keyring

import (
	"errors"
	"fmt"

	gokeyring "github.com/zalando/go-keyring"
)

// Service is the keyring service name for WebUntis passwords; the account is
// the profile name. Other credentials use Service + "-<kind>".
const Service = "webuntis-cli"

// ErrNotFound reports that no secret is stored for the account.
var ErrNotFound = gokeyring.ErrNotFound

// Store reads, writes, and deletes one secret per account.
type Store interface {
	// Get returns the stored secret, or ErrNotFound.
	Get(account string) (string, error)
	// Set stores secret, replacing an existing entry.
	Set(account, secret string) error
	// Delete removes the entry, or returns ErrNotFound.
	Delete(account string) error
}

// System is the Store backed by the operating system keyring.
type System struct {
	// ServiceName overrides the default service for separate stores.
	ServiceName string
}

func (s System) service() string {
	if s.ServiceName != "" {
		return s.ServiceName
	}
	return Service
}

// Get implements Store.
func (s System) Get(account string) (string, error) {
	secret, err := gokeyring.Get(s.service(), account)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", err
		}
		return "", fmt.Errorf("failed to read credentials from the system keyring: %w", err)
	}
	if secret == "" {
		// Only an external tool can store an empty value; treat it as absent.
		return "", ErrNotFound
	}
	return secret, nil
}

// Set implements Store.
func (s System) Set(account, secret string) error {
	if secret == "" {
		return errors.New("cannot store an empty credential in the system keyring")
	}
	if err := gokeyring.Set(s.service(), account, secret); err != nil {
		return fmt.Errorf("failed to store credentials in the system keyring: %w", err)
	}
	return nil
}

// Delete implements Store.
func (s System) Delete(account string) error {
	if err := gokeyring.Delete(s.service(), account); err != nil {
		if errors.Is(err, ErrNotFound) {
			return err
		}
		return fmt.Errorf("failed to remove credentials from the system keyring: %w", err)
	}
	return nil
}
