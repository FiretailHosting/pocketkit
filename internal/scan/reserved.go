package scan

import (
	"fmt"
	"sort"
	"strings"
)

// reservedSegments are the first path segments under /api that PocketBase binds
// itself (apis.bindSettingsApi and friends). A route file that lands on one of
// these compiles fine and then panics at startup with a router pattern conflict,
// which is a miserable way to find out -- so pocketkit refuses at generate time.
var reservedSegments = map[string]string{
	"backups":         "backup management",
	"batch":           "batch API",
	"collections":     "collection and record CRUD",
	"crons":           "cron management",
	"files":           "file serving",
	"health":          "health check",
	"logs":            "request logs",
	"oauth2-redirect": "OAuth2 callback",
	"realtime":        "realtime subscriptions",
	"settings":        "instance settings",
	"sql":             "SQL console",
}

// checkReserved reports an error if urlPath would collide with a built-in route.
func checkReserved(urlPath, file string) error {
	rest, ok := strings.CutPrefix(urlPath, "/api/")
	if !ok {
		return nil
	}
	seg, _, _ := strings.Cut(rest, "/")

	purpose, clash := reservedSegments[seg]
	if !clash {
		return nil
	}

	return fmt.Errorf(
		"%s maps to %s, but PocketBase already serves /api/%s (%s).\n"+
			"       Rename the api/%s directory -- reserved names are: %s",
		file, urlPath, seg, purpose, seg, reservedList())
}

func reservedList() string {
	names := make([]string, 0, len(reservedSegments))
	for n := range reservedSegments {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
