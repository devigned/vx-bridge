package main

import (
	"strings"
	"testing"
)

func TestCachedSetupDoesNotExecuteItsOwnRemoval(t *testing.T) {
	owner := setupSource(t, "../../../installer/setup/launcher.c")
	main := owner[strings.Index(owner, "int WINAPI wWinMain("):]
	refuse := strings.Index(main, "if(remove) {")
	execute := strings.Index(main, "execute(package,quiet)")
	if refuse < 0 || execute <= refuse ||
		!strings.Contains(main[refuse:execute], "return ERROR_CALL_NOT_IMPLEMENTED;") {
		t.Fatal("cached Setup must direct removal to the MSI engine, not delete its mapped image")
	}
	if !strings.Contains(main[refuse:execute], "Installed apps") || strings.Contains(owner, "cache are retained") {
		t.Fatal("normal MSI uninstall must be available and retention must not be reported as success")
	}
}

func TestConventionalRemovalUsesTheSessionNotAnEXEOwnerGrant(t *testing.T) {
	actions := setupSource(t, "../../../installer/setup/actions.c")
	remove := setupSource(t, "../../../installer/setup/remove.c")
	entry := strings.Index(actions, `if(equals(h,L"REMOVE",L"ALL")`)
	pipe := strings.Index(actions, `!setup_property(h,L"VX_SETUP_PIPE",pipe_name`)
	if entry < 0 || pipe <= entry || !strings.Contains(actions[entry:pipe], "setup_remove_authorize(h)") {
		t.Fatal("standalone REMOVE=ALL must have separately scoped actual-session authorization")
	}
	for _, required := range []string{"MsiGetActiveDatabase(h)", "setup_catalog_owned(&c,&p->receipt,&p->receipt)",
		"removal_media(h,&c,&p->receipt)", "MSIINSTALLCONTEXT_USERUNMANAGED", "INSTALLPROPERTY_LOCALPACKAGE",
		"maintenance_properties(h,&c,&p->receipt)", `MsiSetPropertyW(h,L"VX_CACHE_SOURCE",source)`,
		`MsiSetPropertyW(h,L"VX_MAINTENANCE_EXE",owner)`,
		"MsiGetFeatureStateW(h,L\"Bridge\"", "INSTALLSTATE_ABSENT", "wcscmp(root,c.sid)",
		"FOLDERID_RoamingAppData", "FOLDERID_Programs", "Cache.", "setup_owned_quit(",
		`registry_row(db,100,L"Software\\Microsoft\\Windows\\CurrentVersion\\Run",L"VxInfiniteTeesBridge")`,
		"before.nFileIndexHigh==after.nFileIndexHigh", "before.nFileIndexLow==after.nFileIndexLow",
		"INSERT INTO `RemoveFile`", "INSERT INTO `Registry`", " TEMPORARY",
		"MSIRUNMODE_SCHEDULED", "MSIRUNMODE_ROLLBACK", `vx_delete(&c,L"Receipt")`,
		"setup_remove_verify(", "MSIRUNMODE_REBOOTATEND", "uninstall incomplete"} {
		if !strings.Contains(remove, required) {
			t.Fatal("missing conventional removal protection", required)
		}
	}
	for _, forbidden := range []string{"MsiBeginTransaction", "MsiEndTransaction", "MsiDoAction",
		"MsiOpenDatabase", "DuplicateHandle", "TerminateProcess", "CreateProcess", "ShellExecute",
		"RegDeleteTree", "MoveFileEx", "StartupApproved", "getenv("} {
		if strings.Contains(remove, forbidden) {
			t.Fatal("unapproved removal mechanism", forbidden)
		}
	}
	for _, mutator := range []string{"setup_startup(&c,&p->startup,0,FALSE,FALSE)", "setup_startup(&c,&p->startup,0,TRUE,FALSE)"} {
		if strings.Contains(remove, mutator) {
			t.Fatal("removal startup rollback must be owned by MSI, not run when Begin made no change")
		}
	}
	plan := remove[strings.Index(remove, "typedef struct {"):strings.Index(remove, "_Static_assert")]
	if strings.Contains(plan, "HANDLE") {
		t.Fatal("callback plan cannot serialize handles")
	}
	undo := remove[strings.Index(remove, "__stdcall VxRemoveUndo("):strings.Index(remove, "static BOOL empty_key(")]
	if strings.Contains(undo, "vx_open_state(&c,TRUE)") ||
		!strings.Contains(undo, "!registry_inventory(h,&c,p,0)") ||
		!strings.Contains(remove, "VX_STATE_KEY,REG_OPTION_OPEN_LINK,KEY_READ|KEY_WRITE") {
		t.Fatal("rollback must not create/adopt or follow a replaced registry state key")
	}
}

func TestRemovalSequenceAndInventoryAreExplicit(t *testing.T) {
	payload := map[string][]byte{}
	for _, name := range files {
		payload[name] = []byte(name)
	}
	data := author(guid("p"), "0.0.0", strings.Repeat("a", 40), strings.Repeat("b", 64), payload)
	for _, required := range []string{`<EnsureTable Id="RemoveFile"/>`,
		`<Custom Action="VxRemoveUndo" After="InstallInitialize">VX_REMOVE_MODE = "1"`,
		`<Custom Action="VxRemoveBegin" After="VxRemoveUndo">VX_REMOVE_MODE = "1"`,
		`<Custom Action="VxRemoveFinish" After="RemoveFiles">VX_REMOVE_MODE = "1"`,
		`<Custom Action="VxSetupStartup" After="VxSetupUndoStartup">NOT VX_REMOVE_MODE`} {
		if !strings.Contains(data, required) {
			t.Fatal("unsafe or incomplete removal authoring", required)
		}
	}
	private := setupSource(t, "../../../installer/setup/test_remove.c")
	for _, required := range []string{"destination_temp(", "known_file(", "remove_directory(",
		"nonempty", "bridge.5.jsonl", "bridge.4.jsonl", "foreign", ".destination-4294967295.tmp"} {
		if !strings.Contains(private, required) {
			t.Fatal("missing compiled private removal regression", required)
		}
	}
}
