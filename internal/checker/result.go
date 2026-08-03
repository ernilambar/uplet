package checker

// Result is the outcome of a single URL check. The schema is additive-only
// within /v1; a breaking change ships as /v2.
type Result struct {
	URL            string `json:"url"`
	ValidFormat    bool   `json:"valid_format"`
	SiteUp         bool   `json:"site_up"`
	PageExists     bool   `json:"page_exists"`
	StatusCode     int    `json:"status_code"`
	ResponseTimeMs int64  `json:"response_time_ms"`
	Reason         string `json:"reason"`
	ReasonCode     string `json:"reason_code"`
	RedirectedTo   string `json:"redirected_to,omitempty"`
	Attempts       int    `json:"attempts"`
}

// ReasonCode is a stable enum clients match on instead of the free-text
// Reason field.
type ReasonCode string

const (
	ReasonOK               ReasonCode = "ok"
	ReasonNotFound         ReasonCode = "not_found"
	ReasonRedirectNotFound ReasonCode = "redirect_not_found"
	ReasonClientError      ReasonCode = "client_error"
	ReasonServerError      ReasonCode = "server_error"
	ReasonNetworkError     ReasonCode = "network_error"
	ReasonDNSError         ReasonCode = "dns_error"
	ReasonTimeout          ReasonCode = "timeout"
	ReasonInvalidFormat    ReasonCode = "invalid_format"
	ReasonBlockedTarget    ReasonCode = "blocked_target"
	ReasonTooLarge         ReasonCode = "too_large"
	ReasonTLSError         ReasonCode = "tls_error"
)
