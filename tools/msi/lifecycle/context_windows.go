package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"github.com/devigned/vx-bridge/internal/managed"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const installerKey = `Software\VxInfiniteTeesBridge\Installer`
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const faultKey = `Software\VXBridgeLifecycleHarness`
const policyKey = `Software\Policies\Microsoft\Windows\Installer`

var msi = windows.NewLazySystemDLL("msi.dll")
var productInfo = msi.NewProc("MsiGetProductInfoExW")
var enumProducts = msi.NewProc("MsiEnumProductsExW")
var enumClients = msi.NewProc("MsiEnumClientsExW")
var enumComponents = msi.NewProc("MsiEnumComponentsExW")
var openSummary = msi.NewProc("MsiGetSummaryInformationW")
var summaryProperty = msi.NewProc("MsiSummaryInfoGetPropertyW")
var closeMSI = msi.NewProc("MsiCloseHandle")

// userContext is private evidence resolved from the actual primary token and
// loaded user profile, not trusted configuration or inherited harness paths.
type userContext struct {
	// SID is the actual product-operation primary-token user SID.
	SID string `json:"sid"`
	// Account is the resolved domain-qualified token account name.
	Account string `json:"account"`
	// PID is the worker process ID.
	PID int `json:"pid"`
	// Session is the actual token's Windows session ID.
	Session uint32 `json:"session_id"`
	// Elevated is the native TokenElevation DWORD, required to be zero.
	Elevated uint32 `json:"token_elevated"`
	// ElevationType is the native token elevation type, required to be default (1).
	ElevationType uint32 `json:"token_elevation_type"`
	// AdminSIDPresent reports an Administrators SID in token groups, even if
	// disabled; qualification refuses its presence.
	AdminSIDPresent bool `json:"administrators_sid_present_in_token_groups"`
	// Groups contains actual token-group SIDs for private context evidence.
	Groups []string `json:"token_group_sids"`
	// Local is the canonical token-resolved LocalAppData folder.
	Local string `json:"local_appdata"`
	// Roaming is the canonical token-resolved RoamingAppData folder.
	Roaming string `json:"roaming_appdata"`
	// Programs is the canonical token-resolved Start Menu Programs folder.
	Programs string `json:"start_menu_programs"`
	// Profile is the actual canonical loaded user profile directory.
	Profile string `json:"profile"`
	// Root is the fixed managed application directory derived from Local.
	Root string `json:"app_root"`
	// State is the fixed private installer cache/admission directory.
	State string `json:"installer_state"`
	// LocalData is the owned local settings/log/cache parent.
	LocalData string `json:"local_data"`
	// Config is the owned roaming configuration directory.
	Config string `json:"configuration"`
	// InstalledContext names the intended current-user unmanaged MSI context.
	InstalledContext string `json:"intended_msi_context"`
	// Environment records selected actual token-profile environment paths that
	// replace inherited harness values for product children.
	Environment map[string]string `json:"actual_profile_environment,omitempty"`
	// InstallerPolicies records actual registry policy observations for this context.
	InstallerPolicies map[string]policyValue `json:"actual_installer_policies,omitempty"`
}

func tokenDWORD(token windows.Token, kind uint32) (uint32, error) {
	var value, size uint32
	err := windows.GetTokenInformation(token, kind, (*byte)(unsafe.Pointer(&value)), 4, &size)
	if err != nil {
		return 0, err
	}
	if size != 4 {
		return 0, errors.New("unexpected token information size")
	}
	return value, nil
}

func inspectUser(c configuration) (u userContext, result error) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_QUERY|windows.TOKEN_IMPERSONATE|windows.TOKEN_DUPLICATE, &token); err != nil {
		return u, fmt.Errorf("open actual primary token: %w", err)
	}
	defer func() { result = errors.Join(result, token.Close()) }()
	user, err := token.GetTokenUser()
	if err != nil {
		return u, err
	}
	u = userContext{SID: user.User.Sid.String(), PID: os.Getpid(), InstalledContext: "current-user unmanaged (2)"}
	account, domain, _, err := user.User.Sid.LookupAccount("")
	if err != nil {
		return u, err
	}
	u.Account = domain + `\` + account
	u.Elevated, err = tokenDWORD(token, windows.TokenElevation)
	if err != nil {
		return u, err
	}
	u.ElevationType, err = tokenDWORD(token, windows.TokenElevationType)
	if err != nil {
		return u, err
	}
	u.Session, err = tokenDWORD(token, windows.TokenSessionId)
	if err != nil {
		return u, err
	}
	groups, err := token.GetTokenGroups()
	if err != nil {
		return u, err
	}
	for _, group := range groups.AllGroups() {
		sid := group.Sid.String()
		u.Groups = append(u.Groups, sid)
		u.AdminSIDPresent = u.AdminSIDPresent || sid == "S-1-5-32-544"
	}
	if u.SID != c.ExpectedSID || u.SID == c.HarnessSID || !strings.EqualFold(account, c.Account) ||
		u.Elevated != 0 || u.AdminSIDPresent || u.ElevationType != 1 {
		return u, errors.New("product operations require the actual independent standard-account primary token")
	}
	if err := resolveUserFolders(token, &u); err != nil {
		return u, err
	}
	u.Root = filepath.Join(u.Local, "Programs", "VxInfiniteTeesBridge")
	u.LocalData = filepath.Join(u.Local, "VxInfiniteTeesBridge")
	u.State = filepath.Join(u.LocalData, "installer")
	u.Config = filepath.Join(u.Roaming, "VxInfiniteTeesBridge")
	if !within(u.Local, u.Profile) || !within(u.Roaming, u.Profile) || !within(u.Programs, u.Profile) {
		return u, errors.New("fresh standard-account Known Folders must be in its actual loaded profile, never harness paths")
	}
	u.InstallerPolicies, err = executionPolicies()
	if err != nil {
		return u, err
	}
	return u, nil
}

// policyValue records an actual registry policy, distinguishing absence from zero.
type policyValue struct {
	// Present reports that the named policy value was found.
	Present bool `json:"present"`
	// Type is its native registry type when present.
	Type uint32 `json:"registry_type"`
	// Value is its native integer value; execution permits only present DWORD zero,
	// except that AlwaysInstallElevated may be absent.
	Value uint64 `json:"value"`
}

func executionPolicies() (map[string]policyValue, error) {
	proof := map[string]policyValue{}
	for _, policy := range []struct {
		label, name string
		hive        registry.Key
		required    bool
	}{
		{"machine_disable_msi", "DisableMSI", registry.LOCAL_MACHINE, true},
		{"machine_always_install_elevated", "AlwaysInstallElevated", registry.LOCAL_MACHINE, false},
		{"actual_user_always_install_elevated", "AlwaysInstallElevated", registry.CURRENT_USER, false},
	} {
		state := policyValue{}
		key, err := registry.OpenKey(policy.hive, policyKey, registry.QUERY_VALUE|registry.WOW64_64KEY)
		if err == nil {
			state.Value, state.Type, err = key.GetIntegerValue(policy.name)
			state.Present = err == nil
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
			err = errors.Join(err, key.Close())
		} else if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		proof[policy.label] = state
		if err != nil {
			return proof, fmt.Errorf("actual installer policy %s: %w", policy.label, err)
		}
		if (policy.required && !state.Present) || (state.Present && (state.Type != registry.DWORD || state.Value != 0)) {
			return proof, fmt.Errorf("actual installer policy %s must be DWORD0 (absence allowed only for AlwaysInstallElevated): %+v", policy.label, state)
		}
	}
	return proof, nil
}

func resolveUserFolders(token windows.Token, u *userContext) error {
	profile, err := token.GetUserProfileDirectory()
	if err != nil {
		return fmt.Errorf("actual token profile: %w", err)
	}
	u.Profile, err = filepath.EvalSymlinks(profile)
	if err != nil {
		return fmt.Errorf("canonical actual token profile %q: %w", profile, err)
	}
	environment, err := token.Environ(false)
	if err != nil {
		return fmt.Errorf("actual token environment: %w", err)
	}
	values := map[string]string{}
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[strings.ToUpper(key)] = value
		}
	}
	u.Environment = map[string]string{}
	for _, key := range []string{"USERPROFILE", "APPDATA", "LOCALAPPDATA", "TEMP", "TMP"} {
		value := values[key]
		if value == "" || !filepath.IsAbs(value) {
			return fmt.Errorf("actual token %s must be an absolute path", key)
		}
		if key != "TEMP" && key != "TMP" && !within(value, u.Profile) {
			return fmt.Errorf("actual token %s must be within its loaded profile", key)
		}
		// Credential launch may inherit the harness environment. Product children
		// must instead inherit these paths from the actual primary-token profile.
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("bind actual token %s: %w", key, err)
		}
		u.Environment[key] = value
	}
	for _, folder := range []struct {
		name   string
		id     *windows.KNOWNFOLDERID
		target *string
	}{
		{"local_appdata", windows.FOLDERID_LocalAppData, &u.Local},
		{"roaming_appdata", windows.FOLDERID_RoamingAppData, &u.Roaming},
		{"start_menu_programs", windows.FOLDERID_Programs, &u.Programs},
	} {
		path, err := token.KnownFolderPath(folder.id, windows.KF_FLAG_DONT_VERIFY)
		if err != nil {
			return fmt.Errorf("actual token Known Folder %s: %w", folder.name, err)
		}
		*folder.target = path
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("canonical actual token Known Folder %s %q: %w", folder.name, path, err)
		}
		*folder.target = canonical
	}
	return nil
}

// msiPropertyError preserves failed native property retrieval details.
type msiPropertyError struct {
	// Property is the queried MSI property name.
	Property string
	// Code is the actual MSI API return code.
	Code uintptr
	// Size is the returned native buffer length.
	Size uint32
}

// Error formats the property, actual native status, and returned buffer size.
func (e *msiPropertyError) Error() string {
	return fmt.Sprintf("MsiGetProductInfoEx(%s): code=%d size=%d", e.Property, e.Code, e.Size)
}

func unavailableInstalledProperty(property string, err error) bool {
	var status *msiPropertyError
	return property != "State" && errors.As(err, &status) && status.Code == 1608
}

func msiInfo(product, sid, property string) (string, bool, error) {
	code, err := windows.UTF16PtrFromString(product)
	if err != nil {
		return "", false, err
	}
	owner, err := windows.UTF16PtrFromString(sid)
	if err != nil {
		return "", false, err
	}
	name, err := windows.UTF16PtrFromString(property)
	if err != nil {
		return "", false, err
	}
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	status, _, _ := productInfo.Call(uintptr(unsafe.Pointer(code)), uintptr(unsafe.Pointer(owner)), 2,
		uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
	if status == 1605 {
		return "", false, nil
	}
	if status != 0 || size >= uint32(len(buffer)) {
		return "", false, &msiPropertyError{Property: property, Code: status, Size: size}
	}
	return windows.UTF16ToString(buffer[:size]), true, nil
}

func inspectProduct(product string, u userContext) (productState, error) {
	p := productState{Product: product, Properties: map[string]string{}}
	for _, property := range []string{"State", "AssignmentType", "PackageCode", "VersionString", "InstallLocation", "Transforms", "LocalPackage", "ProductName"} {
		value, registered, err := msiInfo(product, u.SID, property)
		if err != nil {
			if p.Registered && unavailableInstalledProperty(property, err) {
				if p.PropertyErrors == nil {
					p.PropertyErrors = map[string]string{}
				}
				p.PropertyErrors[property] = err.Error()
				continue
			}
			return p, err
		}
		if !registered {
			return p, nil
		}
		p.Registered = true
		p.Properties[property] = value
	}
	return p, nil
}

func productInventory(u userContext) ([]productState, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	owner, err := windows.UTF16PtrFromString(u.SID)
	if err != nil {
		return nil, err
	}
	var result []productState
	for index := uint32(0); index < 128; index++ {
		var code [39]uint16
		var sid [256]uint16
		var context uint32
		size := uint32(len(sid))
		status, _, _ := enumProducts.Call(0, uintptr(unsafe.Pointer(owner)), 2, uintptr(index),
			uintptr(unsafe.Pointer(&code[0])), uintptr(unsafe.Pointer(&context)),
			uintptr(unsafe.Pointer(&sid[0])), uintptr(unsafe.Pointer(&size)))
		if status == 259 {
			return result, nil
		}
		if status != 0 || context != 2 || windows.UTF16ToString(sid[:]) != u.SID {
			return nil, fmt.Errorf("MsiEnumProductsEx: code=%d context=%d", status, context)
		}
		p, err := inspectProduct(windows.UTF16ToString(code[:]), u)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return nil, errors.New("unexpected oversized per-user MSI inventory")
}

func cachedPackageCode(path string) (value string, result error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}

	var handle uint32
	status, _, _ := openSummary.Call(0, uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&handle)))
	if status != 0 || handle == 0 {
		return "", fmt.Errorf("cached MSI summary open code=%d", status)
	}
	defer func() {
		code, _, _ := closeMSI.Call(uintptr(handle))
		if code != 0 {
			result = errors.Join(result, fmt.Errorf("cached summary close code=%d", code))
		}
	}()
	var kind uint32
	var integer int32
	var when windows.Filetime
	var buffer [39]uint16
	size := uint32(len(buffer))
	status, _, _ = summaryProperty.Call(uintptr(handle), 9, uintptr(unsafe.Pointer(&kind)),
		uintptr(unsafe.Pointer(&integer)), uintptr(unsafe.Pointer(&when)),
		uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
	if status != 0 || kind != 30 || size >= uint32(len(buffer)) {
		return "", fmt.Errorf("cached MSI actual PackageCode code=%d type=%d size=%d", status, kind, size)
	}
	return windows.UTF16ToString(buffer[:size]), nil
}

// registryState is a bounded shallow HKCU key inventory for private auditing.
type registryState struct {
	// Exists distinguishes an absent key from an empty existing key.
	Exists bool `json:"exists"`
	// Values maps names to native types and raw bytes encoded as hexadecimal.
	Values map[string]registryValue `json:"values"`
	// Keys contains immediate subkey names, not a recursive subtree.
	Keys []string `json:"subkeys"`
}

// registryValue retains native registry bytes without text coercion.
type registryValue struct {
	// Type is the native registry value type.
	Type uint32 `json:"type"`
	// Hex is the lowercase hexadecimal encoding of its raw bytes.
	Hex string `json:"hex"`
}

func inspectRegistry(path string) (s registryState, result error) {
	s.Values = map[string]registryValue{}
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS|registry.WOW64_64KEY)
	if errors.Is(err, registry.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	defer func() { result = errors.Join(result, key.Close()) }()
	s.Exists = true
	names, err := key.ReadValueNames(128)
	if err != nil && err != windows.ERROR_NO_MORE_ITEMS && !errors.Is(err, io.EOF) {
		return s, err
	}
	if len(names) >= 128 {
		return s, errors.New("oversized controlled registry inventory")
	}
	for _, name := range names {
		buffer := make([]byte, 32768)
		n, kind, err := key.GetValue(name, buffer)
		if err != nil || n > len(buffer) {
			return s, fmt.Errorf("registry value inventory %s: %v", name, err)
		}
		s.Values[name] = registryValue{Type: kind, Hex: hex.EncodeToString(buffer[:n])}
	}
	s.Keys, err = key.ReadSubKeyNames(64)
	if err != nil && err != windows.ERROR_NO_MORE_ITEMS && !errors.Is(err, io.EOF) {
		return s, err
	}
	if len(s.Keys) >= 64 {
		return s, errors.New("oversized controlled registry subtree")
	}
	return s, nil
}

func receiptState() (managed.Receipt, bool, bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, installerKey, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if errors.Is(err, registry.ErrNotExist) {
		return managed.Receipt{}, false, false, nil
	}

	if err != nil {
		return managed.Receipt{}, false, false, err
	}
	data := make([]byte, managed.ReceiptSize)
	n, kind, readErr := key.GetValue("Receipt", data)
	_, _, pendingErr := key.GetValue("Pending", nil)
	closeErr := key.Close()
	if errors.Is(readErr, registry.ErrNotExist) {
		if pendingErr != nil && !errors.Is(pendingErr, registry.ErrNotExist) {
			closeErr = errors.Join(closeErr, pendingErr)
		}
		return managed.Receipt{}, false, !errors.Is(pendingErr, registry.ErrNotExist), closeErr
	}
	if err := errors.Join(readErr, closeErr); err != nil {
		return managed.Receipt{}, false, !errors.Is(pendingErr, registry.ErrNotExist), err
	}
	if n != managed.ReceiptSize || kind != registry.BINARY {
		return managed.Receipt{}, false, false, errors.New("installed receipt has wrong actual shape")
	}
	r, err := managed.DecodeReceipt(data)
	if pendingErr != nil && !errors.Is(pendingErr, registry.ErrNotExist) {
		err = errors.Join(err, pendingErr)
	}
	return r, err == nil, !errors.Is(pendingErr, registry.ErrNotExist), err
}
