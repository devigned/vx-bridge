package main

import (
	"os"
	"strings"
	"testing"
)

func setupSource(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPackageIdentityIsEmittedAndNativeMaintenanceRemainsExact(t *testing.T) {
	prepare := setupSource(t, "main.go")
	builder := setupSource(t, "../../../installer/build-setup.ps1")
	common := setupSource(t, "../../../installer/setup/common.c")
	owner := setupSource(t, "../../../installer/setup/launcher.c")
	state := setupSource(t, "../../../installer/managed/state.c")
	remove := setupSource(t, "../../../installer/setup/remove.c")
	if strings.Contains(prepare, `guid("package/`) || strings.Contains(prepare, `"package":`) ||
		!strings.Contains(prepare, `"package_code_policy": "wix-generated"`) {
		t.Fatal("prepared inputs must not guess the final PackageCode")
	}
	for _, required := range []string{"$packageCode = [string](Field $summary 'Property' 9)",
		"$package[0].HasAttribute('Id')", "[Guid]::TryParseExact", "$final['package']=$packageCode",
		"$actualPackage[0].HasAttribute('Id')",
		"$final['msi_sha256']=$hash", "$final['prepared_input_sha256']=$preparedHash",
		"Actual receipt payload hash differs"} {
		if !strings.Contains(builder, required) {
			t.Fatal("emitted package identity lost its byte/source binding", required)
		}
	}
	if strings.Contains(builder, "GetAttribute('Id') -cne $packageCode") {
		t.Fatal("Dark intentionally omits rebuild-specific PackageCode; actual Summary9 is the identity authority")
	}
	for _, pair := range []struct{ code, required string }{
		{common, "PIDSI_REVNUMBER"}, {common, "vx_ascii(r->package,39,text)"},
		{owner, "swprintf(cache_name,ARRAYSIZE(cache_name),L\"%ls.msi\",package)"},
		{owner, "memcmp(&p.before,&target,sizeof(target))"},
		{owner, "z<c"}, {state, "INSTALLPROPERTY_PACKAGECODE"},
		{state, "r->package"}, {remove, "!wcscmp(value,package)"},
	} {
		if !strings.Contains(pair.code, pair.required) {
			t.Fatal("native source/registered/cache identity contract changed", pair.required)
		}
	}
}

func TestNativeSetupPolicyIsWiredNotAnApplicationLaunchAction(t *testing.T) {
	owner := setupSource(t, "../../../installer/setup/launcher.c")
	actions := setupSource(t, "../../../installer/setup/actions.c")
	common := setupSource(t, "../../../installer/setup/common.c")
	protocol := setupSource(t, "../../../installer/setup/protocol.h")
	for _, required := range []string{
		"install_package(path", "Start the bridge now?", "Repair / Reinstall",
		"setup_owned_quit(&context", "setup_payload(&context,&target,FALSE)",
	} {
		if !strings.Contains(owner, required) {
			t.Fatal("missing actual native wiring", required)
		}
		for _, required := range []string{"GetNamedPipeServerProcessId(pipe", "QueryFullProcessImageNameW(process",
			"GetExitCodeProcess(process", `char command[]="quit\n"`} {
			if !strings.Contains(common, required) {
				t.Fatal("shared owned-Quit protocol lost", required)
			}
		}
	}
	for _, text := range []string{owner, actions, common} {
		for _, forbidden := range []string{"TerminateProcess(", "ExitWindowsEx(", "StartupApproved", "ShellExecute", "MoveFileEx"} {
			if strings.Contains(text, forbidden) {
				t.Fatal("unsafe installer operation", forbidden)
			}
		}
	}
	if strings.Contains(actions, "CreateProcess") || strings.Contains(actions, `L"Receipt"`) ||
		strings.Contains(actions, "MsiBeginTransaction") || strings.Contains(actions, "MsiEndTransaction") {
		t.Fatal("MSI callbacks cannot launch applications, publish admission or own/end transactions")
	}
	for _, text := range []string{`L"Auto-launch on Windows Startup"`, `L"Starts when you sign in to Windows."`} {
		if !strings.Contains(protocol, text) {
			t.Fatal("wrong native choice copy", text)
		}
	}
	launch := strings.Index(owner, "if(dialog(L\"Start the bridge now?\"")
	closeLease := strings.Index(owner, "if(lease!=INVALID_HANDLE_VALUE && !CloseHandle(lease))")
	closeState := strings.Index(owner, "if(!vx_close(&context)) result=1;")
	if launch < closeLease || launch < closeState || !strings.Contains(owner, "ready && !quiet") {
		t.Fatal("start must require successful usable completion after installer locks close")
	}
	if !strings.Contains(owner, "NULL,NULL,FALSE,CREATE_NEW_PROCESS_GROUP|CREATE_BREAKAWAY_FROM_JOB") {
		t.Fatal("launch must not inherit installer handles or Setup lifetime")
	}
	launchStart := strings.Index(owner, "static BOOL start_bridge(void)")
	launchEnd := strings.Index(owner, "static int execute(")
	if launchStart < 0 || launchEnd <= launchStart {
		t.Fatal("cannot inspect the ordinary post-setup launch")
	}
	launchBody := owner[launchStart:launchEnd]
	if !strings.Contains(launchBody, `--desktop`) || strings.Contains(launchBody, "--target-port") {
		t.Fatal("Start now must use normal saved-choice startup, not force a destination or IT port")
	}
	if strings.Contains(launchBody, "destination.json") || strings.Contains(launchBody, "DeleteFile") {
		t.Fatal("Start now must not reset or delete the user's game choice")
	}
}

func TestOwnedRootPinsPrecedeEnumerationQuitAndEngine(t *testing.T) {
	owner := setupSource(t, "../../../installer/setup/launcher.c")
	common := setupSource(t, "../../../installer/setup/common.c")
	pin := strings.Index(owner, "if(!setup_pin_root(&context,")
	if pin < 0 {
		t.Fatal("missing native root pin")
	}

	fresh := strings.Index(owner[pin:], "!root_fresh()") + pin
	quit := strings.Index(owner, "!owned_quit(")
	engine := strings.Index(owner, "install=install_package(path")
	if fresh <= pin || quit <= pin || engine <= pin {
		t.Fatal("fixed owned root must be pinned before fresh/repair/Quit/MSI boundaries")
	}
	freshFunction := owner[strings.Index(owner, "static BOOL root_fresh("):strings.Index(owner, "static BOOL result_log(")]
	payloadFunction := common[strings.Index(common, "BOOL setup_payload("):strings.Index(common, "BOOL setup_user(")]
	for _, code := range []string{freshFunction, payloadFunction} {
		if strings.Index(code, "setup_root_pinned(") > strings.Index(code, "FindFirstFileW(") ||
			!strings.Contains(code, "setup_root_pinned(") {
			t.Fatal("enumeration-only root validation reintroduced")
		}
	}
	for _, required := range []string{"vx_pin_tree(c,path)", "pin_readonly_chain(c,parent)", "pin_readonly_chain(c,c->root)",
		"FILE_READ_ATTRIBUTES,FILE_SHARE_READ,OPEN_EXISTING",
		"before.dwVolumeSerialNumber==after.dwVolumeSerialNumber",
		"before.nFileIndexHigh==after.nFileIndexHigh", "before.nFileIndexLow==after.nFileIndexLow",
		"SetFileInformationByHandle(directory,FileDispositionInfo"} {
		if !strings.Contains(common, required) {
			t.Fatal("missing root lifetime/identity boundary", required)
		}
	}
	test := setupSource(t, "../../../installer/setup/test_root.c")
	for _, required := range []string{"FSCTL_SET_REPARSE_POINT", "setup_pin_root(", "setup_payload(&c,&receipt,TRUE)",
		"ERROR_SHARING_VIOLATION", "setup_remove_root(", "MoveFileW(c.root,renamed)"} {
		if !strings.Contains(test, required) {
			t.Fatal("missing private native regression/positive control", required)
		}
	}
}
