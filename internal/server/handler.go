package server

import (
	"encoding/json"
	"mime"
	"net/http"
)

type checkRequest struct {
	URL string `json:"url"`
}

// handleCheck implements POST /v1/check. Content-Type: application/json
// is enforced as a security control (it forces a CORS preflight, so a
// drive-by page can't trigger the outbound-request side effect with a
// simple cross-origin POST) — not merely a formatting nicety. No
// permissive CORS headers are ever sent, so a browser page can't read a
// response even if a preflight somehow got past this check.
func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}

	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type")
		return
	}

	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	default:
		writeError(w, http.StatusTooManyRequests, "too_many_requests")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.MaxBodyBytes)

	var req checkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		writeError(w, http.StatusBadRequest, "missing_url")
		return
	}

	res := s.checker.Check(r.Context(), req.URL)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
