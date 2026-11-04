package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/devigned/vx-bridge/internal/managed"
	"golang.org/x/sys/windows/registry"
)

// fileState describes one observed app-owned or synthetic foreign-sentinel entry.
type fileState struct {
	// Directory reports that the entry is a directory.
	Directory bool `json:"directory"`
	// Bytes is the stat size, not a recursive directory total.
	Bytes int64 `json:"bytes"`
	// SHA256 is the full-file digest for non-directory regular entries.
	SHA256 string `json:"sha256,omitempty"`
}

// snapshot is private servicing evidence captured at a named scenario boundary.
// Capture errors are retained rather than interpreted as clean product state.
type snapshot struct {
	// Name identifies the scenario checkpoint.
	Name string `json:"name"`
	// UTC is its capture-start timestamp.
	UTC string `json:"utc"`
	// Context is the independently inspected actual user/token profile.
	Context userContext `json:"context"`
	// Files maps absolute owned/sentinel paths to bounded file observations.
	Files map[string]fileState `json:"files"`
	// Registry maps audited HKCU paths to shallow raw-value inventories.
	Registry map[string]registryState `json:"registry"`
	// Products contains the user's unmanaged MSI product inventory.
	Products []productState `json:"user_unmanaged_products"`
	// Receipt is the decoded owned receipt when valid, otherwise omitted.
	Receipt *managed.Receipt `json:"receipt,omitempty"`
	// Pending reports durable maintenance denial presence, not successful recovery.
	Pending bool `json:"pending"`
	// StateError contains joined capture failures; partial observations are not a pass.
	StateError string `json:"state_error,omitempty"`
	// ComponentClients maps actual component GUIDs to owning product references.
	ComponentClients map[string][]string `json:"actual_component_client_refs"`
	// FeatureKeypaths contains native feature states and component keypath observations.
	FeatureKeypaths map[string]any `json:"actual_feature_and_component_keypaths"`
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(data, '\n'))
	return errors.Join(writeErr, file.Sync(), file.Close())
}

func collectFiles(root string) (map[string]fileState, error) {
	result := map[string]fileState{}
	_, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if len(result) >= 256 || entry.Type()&os.ModeSymlink != 0 {
			return errors.New("unexpected oversized or reparse audit inventory")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		state := fileState{Directory: info.IsDir(), Bytes: info.Size()}
		if !state.Directory {
			if !info.Mode().IsRegular() {
				return errors.New("nonregular app-managed audit entry")
			}
			state.SHA256, err = hashFile(path)
			if err != nil {
				return err
			}
		}
		result[path] = state
		return nil
	})
	return result, err
}

func capture(name, out string, u userContext, media []media) (s snapshot, result error) {
	s = snapshot{Name: name, UTC: time.Now().UTC().Format(time.RFC3339Nano), Context: u,
		Files: map[string]fileState{}, Registry: map[string]registryState{}}
	for _, root := range append([]string{u.Root, u.LocalData, u.Config,
		filepath.Join(u.Programs, "VX Bridge.lnk"), filepath.Join(u.Programs, "VX Bridge Setup.lnk"),
		filepath.Join(u.Programs, "VX Bridge Diagnostics.lnk")}, foreignFiles(u)...) {
		files, err := collectFiles(root)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("inventory %s: %w", root, err))
		}
		for path, state := range files {
			s.Files[path] = state
		}
	}
	for _, key := range []string{`Software\VxInfiniteTeesBridge`, installerKey,
		`Software\VxInfiniteTeesBridge\MSI`, runKey, faultKey} {
		state, err := inspectRegistry(key)
		if err != nil {
			result = errors.Join(result, err)
		}
		s.Registry[key] = state
	}
	var inventoryErr error
	s.Products, inventoryErr = productInventory(u)
	result = errors.Join(result, inventoryErr)
	s.ComponentClients, inventoryErr = componentClients(u, media)
	result = errors.Join(result, inventoryErr)
	s.FeatureKeypaths, inventoryErr = readFeatureAndKeypaths(u, media)
	result = errors.Join(result, inventoryErr)
	r, valid, pending, err := receiptState()
	if valid {
		s.Receipt = &r
	}
	s.Pending = pending
	if err != nil && s.Registry[installerKey].Exists {
		result = errors.Join(result, err)
	}
	if result != nil {
		s.StateError = result.Error()
	}
	return s, errors.Join(result, writeJSON(filepath.Join(out, name+".json"), s))
}

func verifyInstalled(m media, u userContext, all []media) error {
	p, err := inspectProduct(m.Product, u)
	if err != nil {
		return err
	}
	if !p.Registered || len(p.PropertyErrors) != 0 || p.Properties["State"] != "5" || p.Properties["AssignmentType"] != "0" ||
		p.Properties["PackageCode"] != m.Package || p.Properties["VersionString"] != m.MSIVersion ||
		p.Properties["Transforms"] != "" || !strings.EqualFold(strings.TrimRight(p.Properties["InstallLocation"], `\`), u.Root) {
		return fmt.Errorf("actual registered per-user product differs: %+v", p)
	}
	cached, err := cachedPackageCode(p.Properties["LocalPackage"])
	if err != nil || cached != m.Package {
		return fmt.Errorf("registered OS cache actual PackageCode differs: %s: %v", cached, err)
	}
	inventory, err := productInventory(u)
	if err != nil {
		return err
	}
	count := 0
	knownProducts := map[string]bool{}
	for _, candidate := range all {
		knownProducts[candidate.Product] = true
	}
	for _, p := range inventory {
		if p.Registered && (knownProducts[p.Product] || p.Properties["ProductName"] == "VX Bridge") {
			count++
		}
	}
	if count != 1 {
		return fmt.Errorf("expected exactly one app product, got %d", count)
	}
	clients, err := componentClients(u, all)
	if err != nil {
		return err
	}
	for guid, products := range clients {
		if len(products) != 1 || products[0] != m.Product {
			return fmt.Errorf("actual component %s client refs differ: %v", guid, products)
		}
	}
	entries, err := os.ReadDir(u.Root)
	if err != nil || len(entries) != 6 {
		return fmt.Errorf("installed root must contain exactly six files: %v", err)
	}
	for _, name := range payloadNames {
		hash, err := hashFile(filepath.Join(u.Root, name))
		if err != nil || hash != m.Payload[name] {
			return fmt.Errorf("installed actual payload differs: %s: %v", name, err)
		}
	}
	r, valid, pending, err := receiptState()
	if err != nil || !valid || pending || r.Product != m.Product || r.Package != m.Package ||
		r.Version != m.MSIVersion || r.Revision != m.Source {
		return fmt.Errorf("actual local admission state differs: valid=%t pending=%t receipt=%+v: %v", valid, pending, r, err)
	}
	for index, name := range payloadNames {
		if r.Hashes[index] != m.Payload[name] {
			return errors.New("receipt/payload hash ABI differs")
		}
	}
	if err := verifyPassiveCache(m, u); err != nil {
		return err
	}
	for name, target := range map[string]string{
		"VX Bridge.lnk":             filepath.Join(u.Root, "GSPconnect.exe"),
		"VX Bridge Setup.lnk":       filepath.Join(u.State, "setup.exe"),
		"VX Bridge Diagnostics.lnk": filepath.Join(u.Root, "vxbridge.exe"),
	} {
		arguments := ""
		if name == "VX Bridge Setup.lnk" {
			arguments = "/maintain"
		} else if name == "VX Bridge Diagnostics.lnk" {
			arguments = "--diagnostics-dialog"
		}
		if err := verifyShortcut(filepath.Join(u.Programs, name), target, arguments); err != nil {
			return err
		}
	}
	return nil
}

func verifyPassiveCache(m media, u userContext) error {
	for path, want := range map[string]string{
		filepath.Join(u.State, m.Package+".msi"): m.MSISHA256,
		filepath.Join(u.State, "setup.exe"):      m.SetupSHA256,
	} {
		hash, err := hashFile(path)
		if err != nil || hash != want {
			return fmt.Errorf("actual passive full-media/owner cache differs: %s: %v", path, err)
		}
	}
	cacheKey, err := registry.OpenKey(registry.CURRENT_USER, installerKey, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return err
	}
	for name, want := range map[string]string{"Cache." + m.Package + ".msi": m.MSISHA256, "Cache.setup.exe": m.SetupSHA256} {
		buffer := make([]byte, 65)
		n, kind, readErr := cacheKey.GetValue(name, buffer)
		if readErr != nil || kind != registry.BINARY || n != 65 || string(buffer) != want+"\x00" {
			return errors.Join(fmt.Errorf("recorded full-media cache ownership differs: %s: %v", name, readErr), cacheKey.Close())
		}
	}
	if err := cacheKey.Close(); err != nil {
		return err
	}
	packageCode, err := cachedPackageCode(filepath.Join(u.State, m.Package+".msi"))
	if err != nil || packageCode != m.Package {
		return fmt.Errorf("passive cache actual package differs: %s: %v", packageCode, err)
	}
	return nil
}

func verifyShortcut(path, target, arguments string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("expected real shortcut: %s: %v", path, err)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
	script := "$ErrorActionPreference='Stop'; $s=New-Object -ComObject WScript.Shell; " +
		"$c=$s.CreateShortcut(" + quote(path) + "); " +
		"@{target=$c.TargetPath;arguments=$c.Arguments}|ConvertTo-Json -Compress"
	command := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"),
		"-NoProfile", "-NonInteractive", "-Command", script)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("read shortcut: %w: %s", err, output)
	}
	var actual struct{ Target, Arguments string }
	if err := json.Unmarshal(output, &actual); err != nil {
		return err
	}
	if !strings.EqualFold(actual.Target, target) || actual.Arguments != arguments {
		return fmt.Errorf("shortcut target/arguments differ: %+v", actual)
	}
	return nil
}

func verifyAbsent(u userContext, all []media) error {
	for _, path := range []string{u.Root, u.LocalData, u.Config,
		filepath.Join(u.Programs, "VX Bridge.lnk"), filepath.Join(u.Programs, "VX Bridge Setup.lnk"),
		filepath.Join(u.Programs, "VX Bridge Diagnostics.lnk")} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("uninstall app-owned path remains or cannot be verified: %s: %v", path, err)
		}
	}
	state, err := inspectRegistry(`Software\VxInfiniteTeesBridge`)
	if err != nil || state.Exists {
		return fmt.Errorf("uninstall app-owned registry remains: %+v: %v", state, err)
	}
	legacy, err := inspectRegistry(`Software\Microsoft\Windows\CurrentVersion\Uninstall\VxInfiniteTeesBridge`)
	if err != nil || legacy.Exists {
		return fmt.Errorf("legacy owned ARP key remains: %v", err)
	}
	for _, m := range all {
		if m.Role == "b-fault" {
			continue
		}
		if err := verifyProductAbsent(m, u); err != nil {
			return err
		}
	}
	_, exists, err := ownedRun(u)
	if err != nil || exists {
		return fmt.Errorf("owned sign-in value remains: %v", err)
	}
	inventory, err := productInventory(u)
	if err != nil {
		return err
	}
	for _, product := range inventory {
		if product.Properties["ProductName"] == "VX Bridge" {
			return errors.New("an extra app product registration remains")
		}
	}
	clients, err := componentClients(u, all)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, m := range all {
		known[m.Product] = true
	}
	for guid, products := range clients {
		for _, product := range products {
			if known[product] {
				return fmt.Errorf("owned component client remains: %s/%s", guid, product)
			}
		}
	}
	return nil
}

func ownedRun(u userContext) (string, bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	value, kind, readErr := key.GetStringValue("VxInfiniteTeesBridge")
	closeErr := key.Close()
	if errors.Is(readErr, registry.ErrNotExist) {
		return "", false, closeErr
	}
	if err := errors.Join(readErr, closeErr); err != nil {
		return "", false, err
	}
	if kind != registry.SZ {
		return "", false, errors.New("owned Run value has non-string type")
	}
	return value, true, nil
}
