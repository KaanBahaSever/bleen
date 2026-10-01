//go:build !windows

package platform

import "errors"

// ErrLogonFailed means the server rejected the user name or password.
var ErrLogonFailed = errors.New("E_LOGON_FAILED")
