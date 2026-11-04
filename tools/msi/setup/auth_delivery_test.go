package main

import (
	"strings"
	"testing"
)

func TestOwnedAuthorizationReplyIsConsumedBeforeDisconnect(t *testing.T) {
	server := setupSource(t, "../../../installer/setup/launcher.c")
	client := setupSource(t, "../../../installer/setup/actions.c")
	valid := func(server, client string) bool {
		write := strings.Index(server, "if(!setup_transfer(w->pipe,TRUE,&accepted,sizeof(accepted)))")
		confirm := strings.Index(server, "if(!setup_transfer(w->pipe,FALSE,&confirmed,sizeof(confirmed)) || confirmed!=1)")
		disconnect := strings.Index(server, "if(!DisconnectNamedPipe(w->pipe))")
		read := strings.Index(client, "if(!setup_transfer(pipe,FALSE,&accepted,sizeof(accepted)) || accepted!=1)")
		reply := strings.Index(client, "if(!setup_transfer(pipe,TRUE,&accepted,sizeof(accepted)))")
		return write >= 0 && confirm > write && disconnect > confirm && read >= 0 && reply > read
	}
	if !valid(server, client) || valid(strings.Replace(server, "confirmed!=1", "confirmed!=0", 1), client) ||
		valid(server, strings.Replace(client, "setup_transfer(pipe,TRUE,&accepted", "removed_confirmation(pipe,TRUE,&accepted", 1)) {
		t.Fatal("owned authorization reply can be discarded or acknowledged before it was read")
	}
}

func TestMSIInventoryKeepsEnumerationOnOneThread(t *testing.T) {
	source := setupSource(t, "../lifecycle/context_windows.go")
	start := strings.Index(source, "func productInventory(")
	if start < 0 {
		t.Fatal("MSI inventory/source boundary missing")
	}
	end := strings.Index(source[start:], "func cachedPackageCode(")
	if end < 0 {
		t.Fatal("MSI inventory/source boundary missing")
	}
	body := source[start : start+end]
	lock := strings.Index(body, "runtime.LockOSThread()")
	unlock := strings.Index(body, "defer runtime.UnlockOSThread()")
	call := strings.Index(body, "enumProducts.Call(")
	if lock < 0 || unlock < lock || call < unlock {
		t.Fatal("MsiEnumProductsEx enumeration can migrate OS threads")
	}
}
