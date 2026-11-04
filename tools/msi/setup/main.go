// Setup prepares per-user MSI authoring inputs and packages already authored
// installer media from an authoritatively verified application ZIP.
// It statically checks native PE inputs, source/version/payload identities,
// authoring profiles, and narrowly scoped validation exceptions.
// Preparing or packaging inputs requires clean exact-source HEAD and a new
// absolute external output directory; verification modes inspect existing inputs.
//
// Setup never executes either application, Windows Installer, or the native DLL.
// Native compiler/WiX authoring and guarded disposable-host servicing are separate
// tools. Normal packaging rejects private fault profiles; exceptional fault
// authoring is bound to the exact approved manual qualification context.
// Successful static verification is not live installation or recovery evidence.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

var files = []string{"GSPconnect.exe", "vxbridge.exe", "LICENSE", "THIRD_PARTY_NOTICES.txt", "build-manifest.json", "SHA256SUMS"}
var ids = []string{"VxGUI", "VxControl", "VxLicense", "VxNotices", "VxManifest", "VxSums"}

const upgrade = "{5A5BB970-EB68-4550-AF42-F013F92DB3D8}"

func main() {
	revision := flag.String("revision", "", "clean exact source")
	version := flag.String("version", "", "canonical release version or development")
	archive := flag.String("archive", "", "authoritatively verified application ZIP")
	native := flag.String("native", "", "reviewed native setup output directory")
	out := flag.String("out", "", "new absolute external staging directory")
	checkAuthoring := flag.String("verify-authoring", "", "validate the exact intentional advertise-action omission policy")
	installer := flag.String("installer", "", "package existing statically inspected Windows authoring output")
	copyright := flag.String("copyright", "", "pinned MinGW compiler copyright for installer distribution")
	verifyInstaller := flag.String("verify-installer", "", "verify an existing installer ZIP against the application ZIP")
	profile := flag.String("profile", normalProfile, "normal, or explicitly gated private lifecycle fault authoring")
	flag.Parse()
	if *installer != "" || *verifyInstaller != "" {
		if *profile != normalProfile || *checkAuthoring != "" || *native != "" {
			fmt.Fprintln(os.Stderr, "setup: installer packaging requires only the normal authoring profile")
			os.Exit(1)
		}
		if err := installerPackage(*revision, *version, *archive, *installer, *copyright, *out, *verifyInstaller, flag.NArg()); err != nil {
			fmt.Fprintln(os.Stderr, "setup:", err)
			os.Exit(1)
		}
		return
	}
	if *copyright != "" {
		fmt.Fprintln(os.Stderr, "setup: -copyright requires -installer")
		os.Exit(1)
	}
	if *checkAuthoring != "" {
		info, err := os.Lstat(*checkAuthoring)
		if err == nil && (!info.Mode().IsRegular() || info.Size() > 1<<20) {
			err = errors.New("authoring input must be regular and bounded")
		}
		if err == nil {
			var data []byte
			data, err = os.ReadFile(*checkAuthoring)
			if err == nil {
				err = verifyWarningPolicy(data)
			}
			if err == nil {
				var metadata []byte
				metadata, err = os.ReadFile(filepath.Join(filepath.Dir(*checkAuthoring), "setup-input.json"))
				if err == nil {
					err = verifyValidationPolicy(metadata)
				}
				if err == nil {
					var fields struct {
						Profile string `json:"build_profile"`
						Source  string `json:"source_revision"`
						Version string `json:"version"`
					}
					err = json.Unmarshal(metadata, &fields)
					if err == nil && fields.Profile != *profile {
						err = errors.New("authoring profile differs from explicit builder profile")
					}
					if err == nil {
						err = checkProfileBoundary(*profile, fields.Source, fields.Version, os.Getenv)
					}
					if err == nil {
						err = verifyBuildProfile(data, *profile)
					}
				}
			}
		}
		if flag.NArg() != 0 || *revision != "" || *version != "" || *archive != "" || *native != "" || *out != "" {
			err = errors.New("authoring verification cannot also prepare inputs")
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "setup:", err)
			os.Exit(1)
		}
		return
	}
	if err := checkProfileBoundary(*profile, *revision, *version, os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, "setup:", err)
		os.Exit(1)
	}
	if err := prepare(*revision, *version, *archive, *native, *out, *profile, flag.NArg()); err != nil {
		fmt.Fprintln(os.Stderr, "setup:", err)
		os.Exit(1)
	}
}

var omittedAdvertise = []string{"PublishFeatures", "PublishProduct", "CreateShortcuts"}

func verifyWarningPolicy(data []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var stack []xml.Name
	seen := map[string]bool{}
	sequences := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch element := token.(type) {
		case xml.StartElement:
			parent := xml.Name{}
			if len(stack) != 0 {
				parent = stack[len(stack)-1]
			}
			if len(stack) >= 2 && stack[len(stack)-2].Local == "AdvertiseExecuteSequence" {
				return errors.New("nested content in advertise omissions is refused")
			}
			if element.Name.Local == "AdvertiseExecuteSequence" {
				sequences++
				if parent.Local != "Product" || element.Name.Space != "http://schemas.microsoft.com/wix/2006/wi" {
					return errors.New("unexpected advertise sequence scope")
				}
			}
			allowed := false
			for _, name := range omittedAdvertise {
				allowed = allowed || element.Name.Local == name
			}
			if parent.Local == "InstallExecuteSequence" && allowed {
				return errors.New("normal install publication must use enabled standard defaults")
			}
			isOmission := parent.Local == "AdvertiseExecuteSequence"
			if isOmission {
				if !allowed || seen[element.Name.Local] || element.Name.Space != parent.Space ||
					len(element.Attr) != 1 || element.Attr[0].Name.Local != "Suppress" ||
					element.Attr[0].Name.Space != "" || element.Attr[0].Value != "yes" {
					return errors.New("only the exact three advertise omissions are permitted")
				}
				seen[element.Name.Local] = true
			} else {
				for _, attr := range element.Attr {
					if attr.Name.Local == "Suppress" {
						return errors.New("unexpected suppression outside the reviewed advertise rows")
					}
				}
			}
			if element.Name.Local == "Merge" || element.Name.Local == "MergeRef" {
				return errors.New("merge modules are outside the scoped warning policy")
			}
			stack = append(stack, element.Name)
		case xml.CharData:
			if len(stack) >= 2 && stack[len(stack)-2].Local == "AdvertiseExecuteSequence" &&
				strings.TrimSpace(string(element)) != "" {
				return errors.New("condition-based advertise lookalikes are refused")
			}
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
	if sequences != 1 || len(seen) != 3 {
		return errors.New("require exactly three intentional advertise omissions")
	}
	return nil
}

func verifyValidationPolicy(data []byte) error {
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(data, &metadata); err != nil {
		return err
	}
	var ice []string
	if err := json.Unmarshal(metadata["ice_exceptions"], &ice); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, name := range ice {
		if (name != "ICE91" && name != "ICE96") || seen[name] {
			return errors.New("unexpected or duplicate ICE exception")
		}
		seen[name] = true
	}
	if len(seen) != 2 {
		return errors.New("require only the exact per-user/nonadvertisable ICE exceptions")
	}
	var warnings []map[string]json.RawMessage
	if err := json.Unmarshal(metadata["warning_exceptions"], &warnings); err != nil {
		return err
	}
	if len(warnings) != 1 || len(warnings[0]) != 4 {
		return errors.New("unexpected diagnostic exception policy")
	}
	var policy struct {
		Tool, Table string
		Code        int
		Actions     []string
	}
	record, err := json.Marshal(warnings[0])
	if err != nil {
		return err
	}
	if err := json.Unmarshal(record, &policy); err != nil {
		return err
	}
	if policy.Tool != "light.exe" || policy.Code != 1008 || policy.Table != "AdvtExecuteSequence" || len(policy.Actions) != 3 {
		return errors.New("require only the exact LGHT1008 advertise-omission exception")
	}
	seen = map[string]bool{}
	for _, action := range policy.Actions {
		allowed := false
		for _, expected := range omittedAdvertise {
			allowed = allowed || action == expected
		}
		if !allowed || seen[action] {
			return errors.New("unexpected or duplicate omitted advertise action")
		}
		seen[action] = true
	}
	return nil
}

func numeric(value string) (string, error) {
	if value == "development" {
		return "0.0.0", nil
	}
	if !strings.HasPrefix(value, "v") {
		return "", errors.New("require vMAJOR.MINOR.PATCH or development")
	}
	s := strings.TrimPrefix(value, "v")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return "", errors.New("MSI requires exactly three numeric version fields; no prerelease/fourth field")
	}
	for i, part := range parts {
		bits := 8
		if i == 2 {
			bits = 16
		}
		n, err := strconv.ParseUint(part, 10, bits)
		if err != nil || strconv.FormatUint(n, 10) != part {
			return "", errors.New("MSI version must be canonical and within 255.255.65535")
		}
	}
	return s, nil
}

func guid(domain string) string {
	h := sha256.Sum256([]byte("VxInfiniteTeesBridge/setup/v1/" + domain))
	h[6] = h[6]&15 | 80
	h[8] = h[8]&63 | 128
	return strings.ToUpper(fmt.Sprintf("{%x-%x-%x-%x-%x}", h[:4], h[4:6], h[6:8], h[8:10], h[10:16]))
}
func digest(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func attribute(value string) string {
	var out strings.Builder
	_ = xml.EscapeText(&out, []byte(value))
	return out.String()
}
func command(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}
func prepare(revision, version, archive, native, out, profile string, extra int) error {
	if len(revision) != 40 || strings.Trim(revision, "0123456789abcdef") != "" || extra != 0 {
		return errors.New("require exact lowercase 40-character source revision")
	}
	number, err := numeric(version)
	if err != nil {
		return err
	}
	head, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(head)) != revision {
		return errors.New("source revision differs from HEAD")
	}
	status, err := exec.Command("git", "status", "--porcelain", "--untracked-files=all", "--ignored=matching").Output()
	if err != nil || len(status) != 0 {
		return errors.New("commit clean source first, including ignored files")
	}
	if err := command("go", "run", "-mod=readonly", "./tools/dist", "-revision", revision, "-version", version, "-verify", archive); err != nil {
		return fmt.Errorf("authoritative application verification: %w", err)
	}
	source, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return err
	}
	if !filepath.IsAbs(out) {
		return errors.New("OUT must be a new absolute external directory")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(out))
	if err != nil {
		return err
	}
	out = filepath.Join(parent, filepath.Base(out))
	relative, err := filepath.Rel(strings.TrimSpace(string(source)), out)
	if err != nil || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return errors.New("OUT must be outside source")
	}
	record, err := os.ReadFile(filepath.Join(native, "native-inputs.txt"))
	if err != nil || !strings.HasPrefix(string(record), "source_revision="+revision+"\n") {
		return errors.New("native source identity mismatch")
	}
	setup, err := checkedPE(filepath.Join(native, "setup.exe"), false)
	if err != nil {
		return err
	}
	dll, err := checkedPE(filepath.Join(native, "setup-actions.dll"), true)
	if err != nil {
		return err
	}
	stamp := make([]byte, len(revision)*2+2)
	for i := range revision {
		stamp[i*2] = revision[i]
	}
	if !bytes.Contains(dll, stamp) {
		return errors.New("actual callback DLL source stamp mismatch")
	}
	input, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer input.Close()
	payload := map[string][]byte{}
	for _, entry := range input.File {
		found := false
		for _, name := range files {
			found = found || entry.Name == name
		}
		if !found || entry.UncompressedSize64 > 32<<20 || payload[entry.Name] != nil {
			return errors.New("unexpected, duplicate or oversized input file")
		}
		r, err := entry.Open()
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(r, (32<<20)+1))
		closeErr := r.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return err
		}
		payload[entry.Name] = data
	}
	if len(payload) != len(files) {
		return errors.New("missing authoritative application input")
	}
	if err := os.Mkdir(out, 0o700); err != nil {
		return err
	}
	for name, data := range payload {
		if err := os.WriteFile(filepath.Join(out, name), data, 0o600); err != nil {
			return err
		}
	}
	for name, data := range map[string][]byte{"setup.exe": setup, "setup-actions.dll": dll} {
		if err := os.WriteFile(filepath.Join(out, name), data, 0o600); err != nil {
			return err
		}
	}
	product := guid("product/" + version + "/" + revision)
	xmlSource := author(product, number, revision, digest(setup), payload)
	if profile == lifecycleFaultProfile {
		xmlSource = injectLifecycleFault(xmlSource)
	}
	if err := verifyBuildProfile([]byte(xmlSource), profile); err != nil {
		return err
	}
	if err := verifyWarningPolicy([]byte(xmlSource)); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "payload.wxs"), []byte(xmlSource), 0o600); err != nil {
		return err
	}
	manifest := map[string]any{"schema": 2, "phase": "prepared-input", "package_code_policy": "wix-generated",
		"build_profile":   profile,
		"source_revision": revision, "version": version, "msi_version": number,
		"product": product, "product_code_policy": "standard-major-upgrade",
		"upgrade": upgrade, "setup_sha256": digest(setup), "actions_sha256": digest(dll),
		"warning_exceptions": []any{map[string]any{"tool": "light.exe", "code": 1008,
			"table": "AdvtExecuteSequence", "actions": omittedAdvertise}},
		"ice_exceptions":  []string{"ICE91", "ICE96"},
		"scope":           "LOCAL REVIEW; real setup/MSI source, Windows authoring and behavior unrun; no release activation",
		"startup_default": "off", "auto_launch_label": "Auto-launch on Windows Startup",
		"auto_launch_help": "Starts when you sign in to Windows."}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := verifyValidationPolicy(data); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "setup-input.json"), append(data, '\n'), 0o600)
}

func checkedPE(path string, dll bool) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 2<<20 {
		return nil, errors.New("native input must be regular and bounded")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := checkPEBytes(data, dll); err != nil {
		return nil, err
	}
	return data, nil
}

func checkPEBytes(data []byte, dll bool) error {
	if len(data) == 0 || len(data) > 2<<20 {
		return errors.New("native input must be bounded")
	}
	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer image.Close()
	h, ok := image.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || image.Machine != pe.IMAGE_FILE_MACHINE_AMD64 || (image.Characteristics&pe.IMAGE_FILE_DLL != 0) != dll ||
		(!dll && h.Subsystem != pe.IMAGE_SUBSYSTEM_WINDOWS_GUI) ||
		h.DataDirectory[14].Size != 0 || h.DataDirectory[14].VirtualAddress != 0 ||
		h.DllCharacteristics&(pe.IMAGE_DLLCHARACTERISTICS_DYNAMIC_BASE|pe.IMAGE_DLLCHARACTERISTICS_NX_COMPAT) !=
			pe.IMAGE_DLLCHARACTERISTICS_DYNAMIC_BASE|pe.IMAGE_DLLCHARACTERISTICS_NX_COMPAT {
		return errors.New("require hardened AMD64 native image without CLR")
	}
	imports, err := image.ImportedSymbols()
	if err != nil {
		return err
	}
	for _, imported := range imports {
		name, library, ok := strings.Cut(imported, ":")
		if !ok || !strings.Contains("|kernel32.dll|advapi32.dll|ole32.dll|shell32.dll|comctl32.dll|bcrypt.dll|msi.dll|msvcrt.dll|user32.dll|", "|"+strings.ToLower(library)+"|") {
			return fmt.Errorf("unreviewed native import %s", imported)
		}
		if name == "TerminateProcess" || name == "ExitWindowsEx" || name == "InitiateSystemShutdownW" ||
			(dll && (name == "CreateProcessW" || name == "ShellExecuteW")) {
			return fmt.Errorf("forbidden native import %s", name)
		}
	}
	required := []string{"MsiInstallProductW", "MsiEnumProductsExW", "TaskDialogIndirect", "CreateProcessW"}
	if dll {
		required = []string{"MsiGetActiveDatabase", "MsiGetMode", "MsiProcessMessage", "MsiGetPropertyW",
			"MsiGetProductInfoExW", "MsiGetSummaryInformationW", "MsiDatabaseOpenViewW"}
	}
	required = append(required, "GetNamedPipeServerProcessId", "RegFlushKey")
	for _, name := range required {
		found := false
		for _, imported := range imports {
			found = found || strings.HasPrefix(imported, name+":")
		}
		if !found {
			return fmt.Errorf("missing actual native API %s", name)
		}
	}
	return nil
}

// author emits the fixed per-user nonadvertisable MSI source, retaining ordinary
// install publication and late old-product removal after InstallFinalize.
// The native callbacks enforce authorization and recovery denial; authoring
// alone neither grants servicing permission nor promises atomic old restoration.
func author(product, version, revision, owner string, payload map[string][]byte) string {
	var b strings.Builder
	olderRange := ""
	// The lowest MSI version has no older products; an empty range fails ICE61.
	if version != "0.0.0" {
		olderRange = fmt.Sprintf(`<UpgradeVersion Minimum="0.0.0" IncludeMinimum="yes" Maximum="%s" IncludeMaximum="no" Property="WIX_UPGRADE_DETECTED" MigrateFeatures="yes"/>`+"\n", version)
	}
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="utf-8"?>
<Wix xmlns="http://schemas.microsoft.com/wix/2006/wi">
<Product Id="%s" Name="VX Bridge" Manufacturer="VX Bridge" Language="1033" Version="%s" UpgradeCode="%s">
<Package InstallerVersion="500" Compressed="yes" InstallScope="perUser" InstallPrivileges="limited"/>
<Upgrade Id="%s">
%s<UpgradeVersion Minimum="%s" IncludeMinimum="yes" Property="WIX_DOWNGRADE_DETECTED" OnlyDetect="yes"/>
</Upgrade>
<Condition Message="A newer VX Bridge version is installed.">REMOVE = "ALL" OR Installed OR NOT WIX_DOWNGRADE_DETECTED</Condition>
<MediaTemplate EmbedCab="yes"/>
<Property Id="MSIRESTARTMANAGERCONTROL" Value="Disable"/><Property Id="REBOOT" Value="ReallySuppress"/>
<Property Id="ARPNOMODIFY" Value="1"/><Property Id="ARPNOREPAIR" Value="1"/>
<EnsureTable Id="RemoveFile"/>
<Property Id="VX_SETUP_PROTOCOL" Value="1"/><Property Id="VX_SOURCE_REVISION" Value="%s"/>
<Property Id="VX_OWNER_SHA256" Value="%s"/>
<Condition Message="Run Setup normally as the installing user.">ACTION = "INSTALL" AND NOT ALLUSERS AND NOT MSIINSTALLPERUSER</Condition>
<Condition Message="Rollback support is required.">NOT RollbackDisabled</Condition>
<Condition Message="Patches/transforms and unowned nested installs are unsupported.">NOT PATCH AND NOT TRANSFORMS AND (NOT ParentProductCode OR UPGRADINGPRODUCTCODE)</Condition>
`, product, version, upgrade, upgrade, olderRange, version, revision, owner)
	for i, name := range files {
		property := []string{"GUI", "CONTROL", "LICENSE", "NOTICES", "MANIFEST", "SUMS"}[i]
		fmt.Fprintf(&b, "<Property Id=\"VX_HASH_%s\" Value=\"%s\"/>\n", property, digest(payload[name]))
	}
	b.WriteString(`<Directory Id="TARGETDIR" Name="SourceDir"><Directory Id="LocalAppDataFolder"><Directory Id="INSTALLDIR" Name="VxInfiniteTeesBridge">
`)
	for i, name := range files {
		fmt.Fprintf(&b, `<Component Id="%s" Guid="%s" Win64="yes"><File Id="%s" Name="%s" Source="%s"/><RegistryValue Root="HKCU" Key="Software\VxInfiniteTeesBridge\MSI" Name="%s" Type="string" Value="%s" KeyPath="yes"/></Component>`+"\n",
			ids[i], guid("component/"+name), ids[i], attribute(name), attribute(name), ids[i], revision)
	}
	b.WriteString(`<Component Id="Maintenance" Guid="` + guid("component/maintenance") + `" Win64="yes">
<RemoveFolder Id="VxRemoveOwnedRoot" On="uninstall"/>
<RegistryValue Root="HKCU" Key="Software\VxInfiniteTeesBridge\Installer" Name="SourcePackage" Value="[VX_CACHE_SOURCE]" Type="string" KeyPath="yes"/>
<Shortcut Id="SetupShortcut" Directory="ProgramMenuFolder" Name="VX Bridge Setup" Target="[VX_MAINTENANCE_EXE]" Arguments="/maintain" Advertise="no"/>
</Component>
</Directory></Directory>
<Directory Id="ProgramMenuFolder">
<Component Id="Shortcut" Guid="` + guid("component/shortcut") + `" Win64="yes">
<Shortcut Id="BridgeShortcut" Name="VX Bridge" Target="[INSTALLDIR]GSPconnect.exe" WorkingDirectory="INSTALLDIR" Advertise="no"/>
<RegistryValue Root="HKCU" Key="Software\VxInfiniteTeesBridge\MSI" Name="Shortcut" Type="integer" Value="1" KeyPath="yes"/>
</Component></Directory>
</Directory>
<Feature Id="Bridge" Title="Bridge" Level="1" AllowAdvertise="no">
`)
	for _, id := range append(append([]string{}, ids...), "Shortcut", "Maintenance") {
		fmt.Fprintf(&b, "<ComponentRef Id=\"%s\"/>\n", id)
	}
	b.WriteString(`</Feature>
<Binary Id="SetupActions" SourceFile="setup-actions.dll"/>
<CustomAction Id="VxSetupAuthorize" BinaryKey="SetupActions" DllEntry="VxSetupAuthorize" Return="check"/>
<CustomAction Id="VxSetupUndoStartup" BinaryKey="SetupActions" DllEntry="VxSetupUndoStartup" Execute="rollback" Impersonate="yes" Return="check"/>
<CustomAction Id="VxSetupStartup" BinaryKey="SetupActions" DllEntry="VxSetupStartup" Execute="deferred" Impersonate="yes" Return="check"/>
<CustomAction Id="VxSetupPreFinalize" BinaryKey="SetupActions" DllEntry="VxSetupPreFinalize" Return="check"/>
<CustomAction Id="VxRemoveUndo" BinaryKey="SetupActions" DllEntry="VxRemoveUndo" Execute="rollback" Impersonate="yes" Return="check"/>
<CustomAction Id="VxRemoveBegin" BinaryKey="SetupActions" DllEntry="VxRemoveBegin" Execute="deferred" Impersonate="yes" Return="check"/>
<CustomAction Id="VxRemoveFinish" BinaryKey="SetupActions" DllEntry="VxRemoveFinish" Execute="deferred" Impersonate="yes" Return="check"/>
<CustomAction Id="VxSetLocation" Property="ARPINSTALLLOCATION" Value="[INSTALLDIR]"/>
<CustomAction Id="VxSetRoot" Property="INSTALLDIR" Value="[LocalAppDataFolder]Programs\VxInfiniteTeesBridge\"/>
<InstallExecuteSequence>
<Custom Action="VxSetRoot" Before="CostFinalize">1</Custom>
<Custom Action="VxSetLocation" After="CostFinalize">1</Custom>
<Custom Action="VxSetupAuthorize" Before="InstallValidate">1</Custom>
<RemoveExistingProducts After="InstallFinalize">NOT REMOVE AND WIX_UPGRADE_DETECTED</RemoveExistingProducts>
<Custom Action="VxSetupUndoStartup" After="InstallInitialize">NOT VX_REMOVE_MODE AND NOT UPGRADINGPRODUCTCODE AND NOT VX_PRESERVE_MODE</Custom>
<Custom Action="VxSetupStartup" After="VxSetupUndoStartup">NOT VX_REMOVE_MODE AND NOT UPGRADINGPRODUCTCODE AND NOT VX_PRESERVE_MODE</Custom>
<Custom Action="VxRemoveUndo" After="InstallInitialize">VX_REMOVE_MODE = "1"</Custom>
<Custom Action="VxRemoveBegin" After="VxRemoveUndo">VX_REMOVE_MODE = "1"</Custom>
<Custom Action="VxRemoveFinish" After="RemoveFiles">VX_REMOVE_MODE = "1"</Custom>
<InstallExecute Sequence="6500"/>
<Custom Action="VxSetupPreFinalize" After="InstallExecute">1</Custom>
</InstallExecuteSequence>
<InstallUISequence><Custom Action="VxSetRoot" Before="CostFinalize">1</Custom></InstallUISequence>
<AdminExecuteSequence><LaunchConditions Sequence="100"/></AdminExecuteSequence>
<AdvertiseExecuteSequence>
<PublishFeatures Suppress="yes"/><PublishProduct Suppress="yes"/><CreateShortcuts Suppress="yes"/>
</AdvertiseExecuteSequence>
</Product></Wix>
`)
	return b.String()
}
