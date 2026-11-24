package wa

import (
	"context"
	"log"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
)

// minWAVersion is the newest WhatsApp Web version known to be accepted by the
// server. whatsmeow ships with a fixed version which WhatsApp eventually
// rejects with "Client outdated (405)"; this floor keeps the app working when
// the current version can't be fetched (e.g. started before the network is
// up). Confirmed on 2026-11-24, when the built-in 2.3000.1043636855 was
// already rejected.
var minWAVersion = store.WAVersionContainer{2, 3000, 1056039834}

const versionFetchTimeout = 8 * time.Second

var (
	versionMu      sync.Mutex
	versionFetched bool
)

// fetchLatestVersion is replaced in tests.
var fetchLatestVersion = whatsmeow.GetLatestVersion

func versionLess(a, b store.WAVersionContainer) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// raiseVersion sets v as the announced client version if it is newer than
// the current one. It never downgrades.
func raiseVersion(v store.WAVersionContainer) bool {
	if !versionLess(store.GetWAVersion(), v) {
		return false
	}
	store.SetWAVersion(v)
	return true
}

// EnsureCurrentVersion makes the client announce a WhatsApp Web version the
// server still accepts. It must run before every Connect. The current version
// is fetched from web.whatsapp.com once per run (again when force is set,
// e.g. after a 405); if that fails, the built-in floor is used and the fetch
// is retried on the next call. It reports whether the version was raised.
func EnsureCurrentVersion(ctx context.Context, force bool) bool {
	versionMu.Lock()
	defer versionMu.Unlock()

	changed := raiseVersion(minWAVersion)
	if versionFetched && !force {
		return changed
	}
	fetchCtx, cancel := context.WithTimeout(ctx, versionFetchTimeout)
	defer cancel()
	latest, err := fetchLatestVersion(fetchCtx, nil)
	if err != nil {
		log.Printf("could not fetch the current WhatsApp Web version, using %s: %v", store.GetWAVersion(), err)
		return changed
	}
	versionFetched = true
	if raiseVersion(*latest) {
		log.Printf("using WhatsApp Web version %s", latest)
		changed = true
	}
	return changed
}
