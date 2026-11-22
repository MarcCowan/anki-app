package api

import (
	"net/http"
)

// health reports the build version and the newest daily backup time. It
// needs a working database, so it doubles as the deploy health check.
func (s *server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.Ping(r.Context()); err != nil {
		s.Logger.Error("health: database ping failed", "err", err)
		writeError(w, http.StatusServiceUnavailable, "unavailable", "database unavailable")
		return
	}
	var last *int64
	if s.LastBackup != nil {
		t, ok, err := s.LastBackup()
		switch {
		case err != nil:
			s.Logger.Error("health: reading last backup failed", "err", err)
		case ok:
			ms := t.UnixMilli()
			last = &ms
		}
	}
	writeJSON(w, http.StatusOK, struct {
		Version      string `json:"version"`
		LastBackupAt *int64 `json:"lastBackupAt"`
	}{s.Version, last})
}
