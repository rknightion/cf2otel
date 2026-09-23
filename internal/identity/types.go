// Package identity correlates Access logins with HTTP requests. A match is always inferred.
package identity

import "time"

type Login struct {
	ClientIP, Host, UserEmail, RayID string
	At                               time.Time
}
type Match struct {
	UserEmail, LoginRayID string
	Inferred, Ambiguous   bool
}
type Index interface {
	Observe(Login)
	Lookup(clientIP, host string, at time.Time) Match
}
