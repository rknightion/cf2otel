package selfobs

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"

	"github.com/rknightion/cf2otel/internal/cfapi"
)

// errorClass observes typed failures only. Joined errors use this fixed
// precedence: entitlement, HTTP auth/rate limit, timeout, schema, other.
// Never parse error text: it can contain secrets and has unbounded cardinality.
func errorClass(err error) string {
	var entitlement *cfapi.UnentitledError
	if errors.As(err, &entitlement) {
		return "unentitled"
	}
	var budget *cfapi.GraphQLBudgetError
	if errors.As(err, &budget) {
		return "rate_limited"
	}
	var response *cfapi.HTTPError
	if errors.As(err, &response) {
		switch response.Status {
		case http.StatusTooManyRequests:
			return "rate_limited"
		case http.StatusUnauthorized, http.StatusForbidden:
			return "auth"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return "timeout"
	}
	var fields *cfapi.FieldLimitError
	var syntax *json.SyntaxError
	var shape *json.UnmarshalTypeError
	if errors.As(err, &fields) || errors.As(err, &syntax) || errors.As(err, &shape) {
		return "schema"
	}
	return "other"
}
