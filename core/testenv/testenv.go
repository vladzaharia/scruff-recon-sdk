// Package testenv loads credentials for the SDK integration tests.
//
// The integration tests talk to the real networks using a real account, so they
// are gated twice: behind the `integration` build tag, and behind the presence
// of credentials. `go test ./...` never touches the network.
//
// Run them explicitly:
//
//	go test -tags integration ./sdk/... -v
//
// Credentials come from the environment, or from a `credentials.env` file found
// by walking up from the working directory. That file is gitignored and is not
// part of the repository.
//
//	RECON_EMAIL=...      RECON_PASSWORD=...
//	SCRUFF_EMAIL=...     SCRUFF_PASSWORD=...
//
// Optional, for the write-side tests:
//
//	SMOKE_ALLOW_WRITES=1        # enable tests that send or otherwise mutate
//	RECON_TEST_PEER=<profileId> # who to message on Recon
//	SCRUFF_TEST_PEER=<profileId># who to message on SCRUFF
//
// Nothing here logs a credential, and callers should keep it that way: a failed
// smoke test should print a status code, never a token.
package testenv

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const credentialsFile = "credentials.env"

var loadOnce sync.Once

// load reads credentials.env into the environment, without overwriting
// variables that are already set.
func load() {
	loadOnce.Do(func() {
		path, ok := findUp(credentialsFile)
		if !ok {
			return
		}
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()

		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, found := strings.Cut(line, "=")
			if !found {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			if _, exists := os.LookupEnv(k); !exists {
				_ = os.Setenv(k, v)
			}
		}
	})
}

// findUp walks up from the working directory looking for name.
func findUp(name string) (string, bool) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// Get returns an environment value, loading credentials.env first.
func Get(key string) string {
	load()
	return os.Getenv(key)
}

// Credentials is an email/password pair for one network.
type Credentials struct {
	Email    string
	Password string
}

// Require returns the credentials for prefix ("RECON", "SCRUFF"), skipping the
// test when they are absent.
//
// Skipping rather than failing is deliberate: a contributor without an account
// on a given network should be able to run the rest of the suite.
func Require(t *testing.T, prefix string) Credentials {
	t.Helper()
	c := Credentials{
		Email:    Get(prefix + "_EMAIL"),
		Password: Get(prefix + "_PASSWORD"),
	}
	if c.Email == "" || c.Password == "" {
		t.Skipf("no %s credentials: set %s_EMAIL and %s_PASSWORD, or add them to %s",
			prefix, prefix, prefix, credentialsFile)
	}
	return c
}

// WritesAllowed reports whether tests that mutate remote state may run.
//
// Anything that sends a message, woofs, blocks, reports, or RSVPs must be
// guarded by this. Those actions are visible to other people and are not
// undoable, so they stay off unless explicitly enabled.
func WritesAllowed() bool {
	v := Get("SMOKE_ALLOW_WRITES")
	return v == "1" || strings.EqualFold(v, "true")
}

// RequireWrites skips the test unless writes are enabled.
func RequireWrites(t *testing.T) {
	t.Helper()
	if !WritesAllowed() {
		t.Skip("write test: set SMOKE_ALLOW_WRITES=1 to enable (this contacts real people)")
	}
}

// Peer returns the test peer profile id for prefix, skipping if unset.
func Peer(t *testing.T, prefix string) string {
	t.Helper()
	p := Get(prefix + "_TEST_PEER")
	if p == "" {
		t.Skipf("no test peer: set %s_TEST_PEER to a profile id", prefix)
	}
	return p
}
