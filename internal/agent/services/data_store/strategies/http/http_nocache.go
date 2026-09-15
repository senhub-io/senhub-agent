package http

import "net/http"

// NoStore tells every client and intermediary that what this server
// answers must not be reused.
//
// The endpoints here return the current value of a measurement. They
// carry no validator and no expiry, which leaves a client free to apply
// its own heuristic freshness and serve an earlier answer: a poller
// would then chart a value the agent never reported at that instant,
// and nothing on either side would say so. A stale metric is worse than
// a missing one, because it looks like an observation.
//
// Handlers that want a different policy set the header themselves and
// win, since they run after this and Set replaces: the embedded console
// assets do exactly that, where revalidation is both safe and useful.
func NoStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
