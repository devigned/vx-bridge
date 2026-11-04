package main

import (
	"strings"
	"testing"
)

func TestSingleMSICallHandsOffOnlyRootAndVerifiesTerminalState(t *testing.T) {
	owner := setupSource(t, "../../../installer/setup/launcher.c")
	if strings.Count(owner, "install=install_package(") != 1 {
		t.Fatal("maintenance must execute exactly one conventional MSI operation")
	}
	call := strings.Index(owner, "install=install_package(path")
	handoff := strings.Index(owner, "setup_release_root(&context,&root_slot,&root_before)")
	post := strings.Index(owner, "setup_repin_root(&context,&root_slot,NULL)")
	if handoff < 0 || call <= handoff || post <= call ||
		!strings.Contains(owner[call:post], "if(install!=ERROR_SUCCESS) goto done;") {
		t.Fatal("MSI root handoff and terminal success boundary missing")
	}
	for _, forbidden := range []string{"MsiBeginTransaction", "MsiEndTransaction", "setup_recreate_root(", "rollback:"} {
		if strings.Contains(owner, forbidden) {
			t.Fatal("discarded custom transaction/compensation remains", forbidden)
		}
	}
	common := setupSource(t, "../../../installer/setup/common.c")
	release := common[strings.Index(common, "BOOL setup_release_root("):strings.Index(common, "BOOL setup_repin_root(")]
	if !strings.Contains(release, "same_directory(&actual,identity)") ||
		!strings.Contains(release, "CloseHandle(c->pins[*slot])") || strings.Contains(release, "vx_close(") {
		t.Fatal("handoff must retain validated ancestors/source exclusion")
	}
}

func TestFailureRetainsDenialWithoutPreviousVersionOrRootIdentityRequirement(t *testing.T) {
	owner := setupSource(t, "../../../installer/setup/launcher.c")
	body := owner[strings.Index(owner, "static int execute("):strings.Index(owner, "int WINAPI wWinMain(")]
	done := body[strings.Index(body, "\ndone:"):]
	if strings.Contains(done, `vx_delete(&context,L"Pending")`) ||
		!strings.Contains(done, "install==(UINT)-1 && !ready && root_fresh()") ||
		!strings.Contains(done, "Repair cannot complete") {
		t.Fatal("failed operation must retain denial and give supported recovery guidance")
	}
	if strings.Count(body, `vx_delete(&context,L"Pending")`) != 1 ||
		strings.Index(body, `vx_put(&context,L"Receipt"`) >= strings.Index(body, `vx_delete(&context,L"Pending")`) {
		t.Fatal("only verified receipt publication may clear installation denial")
	}
}
