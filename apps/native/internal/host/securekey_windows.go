package host

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	advapi32  = windows.NewLazySystemDLL("advapi32.dll")
	credReadW = advapi32.NewProc("CredReadW")
	credFree  = advapi32.NewProc("CredFree")
)

// credential is Win32's CREDENTIALW.
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

const credTypeGeneric = 1

// securityKey reads a key the CLI keeps in the Credential Manager, where
// keytar files it as the generic credential "<service>/<account>" holding
// the key's UTF-8 text.
func securityKey(account string) string {
	target, err := windows.UTF16PtrFromString(keychainService + "/" + account)
	if err != nil {
		return ""
	}
	var c *credential
	if ok, _, _ := credReadW.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&c))); ok == 0 || c == nil {
		return ""
	}
	defer credFree.Call(uintptr(unsafe.Pointer(c)))
	if c.CredentialBlob == nil {
		return ""
	}
	return string(unsafe.Slice(c.CredentialBlob, c.CredentialBlobSize))
}
