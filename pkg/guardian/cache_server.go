package guardian

import (
	"context"
	"net/http"
)

// CacheServer is an HTTP server for manipulating Guardian's In Memory Cache.
// Useful for e2e testing where the Guardian server might be running in a separate process.
type CacheServer struct {
	rc     *RedisCounter
	server *http.Server
}

func NewCacheServer(address string, rc *RedisCounter) *CacheServer {
	mux := http.NewServeMux()
	server := &http.Server{
		Addr:    address,
		Handler: mux,
	}
	cs := &CacheServer{
		server: server,
		rc:     rc,
	}
	mux.HandleFunc("/v0/counters", cs.DeleteCounters)
	return cs
}

func (cs *CacheServer) Run(stop <-chan struct{}) error {
	var err error
	errChan := make(chan error, 1)
	go func() {
		errChan <- cs.server.ListenAndServe()
	}()
	select {
	case <-stop:
	case serverError := <-errChan:
		err = serverError
	}
	cs.server.Shutdown(context.TODO())
	return err
}

func (cs *CacheServer) DeleteCounters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not accepted", 405)
	}
	cs.rc.removeAll()
	w.WriteHeader(200)
}
