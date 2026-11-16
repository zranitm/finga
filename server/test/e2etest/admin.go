package e2etest

import (
	"errors"
	"net/http"
	"testing"

	"finga/internal/shared/adminapi"
)

// Status is the admin API's answer to err: its status and reason, or 0 for none.
func Status(err error) (int, string) {
	if e, ok := errors.AsType[adminapi.StatusError](err); ok {
		return e.Status, e.Reason
	}
	return 0, ""
}

// Session signs username up and logs in: the session's token.
func (s *Stack) Session(t *testing.T, username string) string {
	t.Helper()
	s.MustStatus(s.Call("POST", "/api/accounts", "", map[string]string{"username": username, "password": "correct horse"}), http.StatusCreated)
	return s.Call("POST", "/api/sessions", "", map[string]string{"username": username, "password": "correct horse"}).Body["token"].(string)
}
