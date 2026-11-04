package main

import (
	"strings"
	"testing"
)

func TestRecoveryUsesOneVerifiedSelectedPackageAndPreservesDenial(t *testing.T) {
	source := setupSource(t, "../../../installer/setup/launcher.c")
	for _, required := range []string{
		"!vx_pending(&stale) || memcmp(&stale.after,&target,sizeof(target))",
		"strcmp(p.before.revision,target.revision)",
		"setup_catalog_owned(&context,old_exists?&p.before:&target,&target)",
		"setup_related_owned(&context,old_exists?&p.before:NULL,&target)",
		"maintenance=L\"REINSTALL=ALL REINSTALLMODE=amus\"",
		"maintenance=L\"ADDLOCAL=Bridge REINSTALLMODE=amus\"",
		"if(install!=ERROR_SUCCESS) goto done;",
		"setup_payload(&context,&p.before,TRUE)",
		"!registered(&target,FALSE) || !setup_single_product(&target) || !setup_payload(&context,&target,FALSE)",
		"vx_put(&context,L\"Receipt\",&target",
		"vx_delete(&context,L\"Pending\")",
	} {
		if !strings.Contains(source, required) {
			t.Fatal("recovery-first ownership/postcondition lost", required)
		}
	}
	for _, forbidden := range []string{"advertised_before", "reconcile_before", "install_package(old", "MsiAdvertiseProduct", "RegDeleteTree", "RegCreateKey", "MsiBeginTransaction", "MsiEndTransaction"} {
		if strings.Contains(source, forbidden) {
			t.Fatal("discarded compensation or ownership bypass", forbidden)
		}
	}
}

func TestRecoveryAdmissionChecksCatalogNotOnlyUpgradeLinks(t *testing.T) {
	common := setupSource(t, "../../../installer/setup/common.c")
	body := common[strings.Index(common, "BOOL setup_single_product("):strings.Index(common, "BOOL setup_related_owned(")]
	if !strings.Contains(body, "MsiEnumProductsExW(NULL,NULL,MSIINSTALLCONTEXT_USERUNMANAGED") ||
		!strings.Contains(body, `L"ProductName"`) || !strings.Contains(body, "return count==1") ||
		strings.Contains(body, "MsiEnumRelatedProducts") {
		t.Fatal("advertised remnants without restored UpgradeCodes must still block successful admission")
	}
}

func TestRecoveryKeepsActualSourceAndOperationBinding(t *testing.T) {
	actions := setupSource(t, "../../../installer/setup/actions.c")
	for _, required := range []string{"pending.phase!=VX_MUTATING", "memcmp(&pending.after,&receipt,sizeof(receipt))",
		"wcscmp(target,operation_nonce)"} {
		if !strings.Contains(actions, required) {
			t.Fatal("recovery must remain actual owned Pending/source/operation", required)
		}
		if strings.Contains(actions, `MsiSetPropertyW(h,L"WIX_UPGRADE_DETECTED"`) {
			t.Fatal("maintenance cannot pretend to run first-install-only RemoveExistingProducts")
		}
	}
	if strings.Contains(actions, "MsiSetComponentState") || strings.Contains(actions, "MsiDoAction") ||
		strings.Contains(actions, "RegDeleteTree") {
		t.Fatal("late upgrade must stay standard MSI sequencing, not component or registration reset")
	}
}

func TestStandardUpgradeRemovalBorrowsOnlyVerifiedQuiescedParent(t *testing.T) {
	actions := setupSource(t, "../../../installer/setup/actions.c")
	for _, required := range []string{"pending.phase!=VX_MUTATING", "memcmp(&pending.before,&receipt,sizeof(receipt))",
		`setup_property(h,L"UPGRADINGPRODUCTCODE",parent,39)`, "wcscmp(parent,expected)",
		"vx_registered(&c,&receipt,FALSE)", "setup_payload(&c,&receipt,TRUE)",
		"OpenMutexW(SYNCHRONIZE|MUTEX_MODIFY_STATE", "wait!=WAIT_TIMEOUT",
		"GetLastError()==ERROR_SHARING_VIOLATION", "setup_upgrade_authorize(h)"} {
		if !strings.Contains(actions, required) {
			t.Fatal("standard upgrade lost actual parent/source/quiescence admission", required)
		}
	}
}
