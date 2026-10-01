//go:build windows

package platform

import (
	"errors"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modAdvapi32           = windows.NewLazySystemDLL("advapi32.dll")
	procCredWriteW        = modAdvapi32.NewProc("CredWriteW")
	modMpr                = windows.NewLazySystemDLL("mpr.dll")
	procWNetAddConnection = modMpr.NewProc("WNetAddConnection2W")
	procWNetCancelConn    = modMpr.NewProc("WNetCancelConnection2W")
)

// ErrLogonFailed means the server rejected the user name or password.
var ErrLogonFailed = errors.New("E_LOGON_FAILED")

type netResource struct {
	Scope, Type, DisplayType, Usage uint32
	LocalName, RemoteName           *uint16
	Comment, Provider               *uint16
}

type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

const (
	resourceTypeDisk               = 1
	credTypeDomainPassword         = 2
	credPersistLocalMachine        = 2
	errorSessionCredentialConflict = 1219
	errorLogonFailure              = 1326
)

// SaveShareCredential connects to \\server\share as user and stores the
// password in Windows Credential Manager, so the share stays reachable after
// a restart. bleen itself never stores the password.
func SaveShareCredential(server, share, user, password string) error {
	remote, _ := windows.UTF16PtrFromString(`\\` + server + `\` + share)
	pUser, _ := windows.UTF16PtrFromString(user)
	pPass, _ := windows.UTF16PtrFromString(password)
	nr := netResource{Type: resourceTypeDisk, RemoteName: remote}
	connect := func() uintptr {
		r, _, _ := procWNetAddConnection.Call(uintptr(unsafe.Pointer(&nr)),
			uintptr(unsafe.Pointer(pPass)), uintptr(unsafe.Pointer(pUser)), 0)
		return r
	}
	r := connect()
	if r == errorSessionCredentialConflict {
		// Windows allows one identity per server and session: drop the old one.
		procWNetCancelConn.Call(uintptr(unsafe.Pointer(remote)), 0, 1)
		r = connect()
	}
	switch r {
	case 0:
	case errorLogonFailure, uintptr(windows.ERROR_ACCESS_DENIED):
		return ErrLogonFailed
	default:
		return windows.Errno(r)
	}

	target, _ := windows.UTF16PtrFromString(server)
	blob := utf16.Encode([]rune(password))
	cred := credential{
		Type:       credTypeDomainPassword,
		TargetName: target,
		Persist:    credPersistLocalMachine,
		UserName:   pUser,
	}
	if len(blob) > 0 {
		cred.CredentialBlobSize = uint32(len(blob) * 2)
		cred.CredentialBlob = (*byte)(unsafe.Pointer(&blob[0]))
	}
	if ok, _, err := procCredWriteW.Call(uintptr(unsafe.Pointer(&cred)), 0); ok == 0 {
		return err
	}
	return nil
}
