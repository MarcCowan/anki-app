package api

import (
	"net/http"

	"anki-app/server/internal/store"
	"anki-app/server/internal/validate"
)

type settingsDTO struct {
	DesiredRetention float64 `json:"desiredRetention"`
	DayRolloverHour  int     `json:"dayRolloverHour"`
}

func toSettingsDTO(st store.Settings) settingsDTO {
	return settingsDTO{DesiredRetention: st.DesiredRetention, DayRolloverHour: st.DayRolloverHour}
}

func (s *server) getSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.Store.GetSettings(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSettingsDTO(st))
}

func (s *server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DesiredRetention *float64 `json:"desiredRetention"`
		DayRolloverHour  *int     `json:"dayRolloverHour"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	st, err := s.Store.GetSettings(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if in.DesiredRetention != nil {
		if err := validate.DesiredRetention(*in.DesiredRetention); err != nil {
			s.fail(w, r, err)
			return
		}
		st.DesiredRetention = *in.DesiredRetention
	}
	if in.DayRolloverHour != nil {
		if err := validate.RolloverHour(*in.DayRolloverHour); err != nil {
			s.fail(w, r, err)
			return
		}
		st.DayRolloverHour = *in.DayRolloverHour
	}
	if err := s.Store.UpdateSettings(r.Context(), st); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSettingsDTO(st))
}
