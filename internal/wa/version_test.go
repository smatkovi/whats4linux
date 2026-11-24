package wa

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"go.mau.fi/whatsmeow/store"
)

// withVersionState runs f with a stubbed fetcher and restores the global
// whatsmeow version and fetch state afterwards.
func withVersionState(t *testing.T, start store.WAVersionContainer, fetch func() (*store.WAVersionContainer, error), f func(calls *int)) {
	t.Helper()
	oldVersion, oldFetch, oldFetched := store.GetWAVersion(), fetchLatestVersion, versionFetched
	t.Cleanup(func() {
		store.SetWAVersion(oldVersion)
		fetchLatestVersion = oldFetch
		versionFetched = oldFetched
	})
	store.SetWAVersion(start)
	versionFetched = false
	calls := 0
	fetchLatestVersion = func(context.Context, *http.Client) (*store.WAVersionContainer, error) {
		calls++
		return fetch()
	}
	f(&calls)
}

var builtIn = store.WAVersionContainer{2, 3000, 1043636855}

func TestEnsureCurrentVersionUsesFetchedVersion(t *testing.T) {
	latest := store.WAVersionContainer{2, 3000, 1060000000}
	withVersionState(t, builtIn, func() (*store.WAVersionContainer, error) { return &latest, nil }, func(calls *int) {
		if !EnsureCurrentVersion(context.Background(), false) {
			t.Fatal("version not reported as changed")
		}
		if got := store.GetWAVersion(); got != latest {
			t.Fatalf("version = %s, want %s", got, latest)
		}
		// Fetched once per run unless forced.
		EnsureCurrentVersion(context.Background(), false)
		if *calls != 1 {
			t.Fatalf("fetched %d times, want 1", *calls)
		}
		if EnsureCurrentVersion(context.Background(), true) {
			t.Fatal("forced refetch of the same version reported a change")
		}
		if *calls != 2 {
			t.Fatalf("forced call fetched %d times in total, want 2", *calls)
		}
	})
}

func TestEnsureCurrentVersionFallsBackToFloorAndRetries(t *testing.T) {
	withVersionState(t, builtIn, func() (*store.WAVersionContainer, error) { return nil, errors.New("offline") }, func(calls *int) {
		if !EnsureCurrentVersion(context.Background(), false) {
			t.Fatal("floor not applied")
		}
		if got := store.GetWAVersion(); got != minWAVersion {
			t.Fatalf("version = %s, want the floor %s", got, minWAVersion)
		}
		// A failed fetch is retried on the next call.
		EnsureCurrentVersion(context.Background(), false)
		if *calls != 2 {
			t.Fatalf("fetched %d times, want 2", *calls)
		}
	})
}

func TestEnsureCurrentVersionNeverDowngrades(t *testing.T) {
	newer := store.WAVersionContainer{2, 3000, 1099999999}
	older := store.WAVersionContainer{2, 3000, 1000000000}
	withVersionState(t, newer, func() (*store.WAVersionContainer, error) { return &older, nil }, func(*int) {
		if EnsureCurrentVersion(context.Background(), false) {
			t.Fatal("reported a change for an older version")
		}
		if got := store.GetWAVersion(); got != newer {
			t.Fatalf("version = %s, want %s kept", got, newer)
		}
	})
}

func TestVersionLess(t *testing.T) {
	cases := []struct {
		a, b store.WAVersionContainer
		want bool
	}{
		{store.WAVersionContainer{2, 3000, 1}, store.WAVersionContainer{2, 3000, 2}, true},
		{store.WAVersionContainer{2, 3000, 2}, store.WAVersionContainer{2, 3000, 1}, false},
		{store.WAVersionContainer{2, 2999, 9}, store.WAVersionContainer{2, 3000, 1}, true},
		{store.WAVersionContainer{2, 3000, 1}, store.WAVersionContainer{2, 3000, 1}, false},
	}
	for _, c := range cases {
		if got := versionLess(c.a, c.b); got != c.want {
			t.Errorf("versionLess(%s, %s) = %v", c.a, c.b, got)
		}
	}
}
