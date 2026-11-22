package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"anki-app/server/internal/validate"
)

// MaxBodyBytes caps request bodies (spec §6: ~14× the largest legitimate request).
const MaxBodyBytes = 1 << 20

// decodeJSON reads exactly one JSON value into dst, rejecting unknown fields,
// trailing data and bodies over MaxBodyBytes.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return bodyErr(err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%w: request body must contain a single JSON value", validate.ErrInvalid)
		}
		return bodyErr(err)
	}
	return nil
}

func bodyErr(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return err
	}
	return fmt.Errorf("%w: request body is not valid JSON for this endpoint (%s)", validate.ErrInvalid, err.Error())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
