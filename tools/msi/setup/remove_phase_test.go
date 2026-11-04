package main

import (
	"strings"
	"testing"
)

func TestRemovalRegistrationAdmissionPrecedesEngineUnpublication(t *testing.T) {
	source := setupSource(t, "../../../installer/setup/remove.c")
	authorize := strings.Index(source, "UINT setup_remove_authorize(")
	begin := strings.Index(source, "__stdcall VxRemoveBegin(")
	undo := strings.Index(source, "__stdcall VxRemoveUndo(")
	finish := strings.Index(source, "static BOOL empty_key(")
	if authorize < 0 || begin < authorize || undo < begin || finish < undo {
		t.Fatal("removal source boundaries missing")
	}
	if !strings.Contains(source[authorize:begin], "!setup_catalog_owned(&c,&p->receipt,&p->receipt)") ||
		!strings.Contains(source[authorize:begin], "!removal_media(h,&c,&p->receipt)") {
		t.Fatal("immediate actual owned catalog/cache or interrupted-source authorization missing")
	}
	for _, body := range []string{source[begin:undo], source[undo:finish]} {
		for _, required := range []string{"!decode(h,p)", "!session_user(h,&c,p)", "owner(&c)", "registry_inventory(h,&c,p,0)"} {
			if !strings.Contains(body, required) {
				t.Fatal("deferred/rollback plan/session/ownership admission missing", required)
			}
		}
		if strings.Contains(body, "vx_registered(") {
			t.Fatal("deferred callback requires fully installed registration after engine unpublication")
		}
	}
}

func TestShortcutOwnershipReadRetainsExactDeleteHandoffChecks(t *testing.T) {
	source := setupSource(t, "../../../installer/setup/remove.c")
	for _, required := range []string{
		"BOOL recheck_shortcut=exclusive && directory==REMOVE_SHORTCUTS;",
		"access=recheck_shortcut?GENERIC_READ:exclusive?GENERIC_READ|DELETE",
		"ok=shortcut_owned(c,name,path);",
		"GetFileInformationByHandle(file,&before) && vx_hash(file,actual)",
		"before.dwVolumeSerialNumber==after.dwVolumeSerialNumber",
		"before.nFileIndexHigh==after.nFileIndexHigh && before.nFileIndexLow==after.nFileIndexLow",
		"vx_hash(file,want) && !strcmp(actual,want)",
	} {
		if !strings.Contains(source, required) {
			t.Fatal("shortcut delete admission lost its read/ID/byte proof", required)
		}
	}
}
