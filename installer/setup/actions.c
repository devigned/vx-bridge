/* SPDX-License-Identifier: MIT */
#include "protocol.h"
#include <objbase.h>
#ifndef VX_SOURCE_REVISION
#error Exact callback source revision required
#endif
#define VX_WIDE_INNER(x) L##x
#define VX_WIDE(x) VX_WIDE_INNER(x)
static const WCHAR callback_revision[]=VX_WIDE(VX_SOURCE_REVISION);

UINT setup_upgrade_authorize(MSIHANDLE h)
{
	VX_CONTEXT c; VX_RECEIPT receipt; VX_PENDING pending; WCHAR owner_hash[65],parent[39],expected[39],path[MAX_PATH];
	MSIHANDLE db=0; HANDLE lock=NULL,lease=INVALID_HANDLE_VALUE; BOOL ok=FALSE,exists=FALSE;
	if(!vx_context(&c)) return ERROR_INSTALL_FAILURE;
	if(!(db=MsiGetActiveDatabase(h)) || !setup_receipt(db,&receipt,owner_hash) ||
		!vx_open_state(&c,FALSE) || !vx_binary(&c,L"Pending",&pending,sizeof(pending),&exists) ||
		!exists || !vx_pending(&pending) || pending.phase!=VX_MUTATING || !pending.has_before ||
		memcmp(&pending.before,&receipt,sizeof(receipt)) || strcmp(pending.after.revision,receipt.revision) ||
		!vx_wide(expected,39,pending.after.product) ||
		!setup_property(h,L"UPGRADINGPRODUCTCODE",parent,39) || wcscmp(parent,expected) ||
		!vx_registered(&c,&receipt,FALSE) || !setup_pin_directory(&c,c.root) ||
		!setup_payload(&c,&receipt,TRUE)) goto done;
	/* Standard RemoveExistingProducts borrows the parent's already-quiesced
	 * operation. It must not wipe settings/cache or mutate startup/Receipt.
	 */
	WCHAR name[320];
	if(swprintf(name,ARRAYSIZE(name),L"Global\\VxBridge.SetupOwner.%ls",c.sid)<=0 ||
		!(lock=OpenMutexW(SYNCHRONIZE|MUTEX_MODIFY_STATE,FALSE,name))) goto done;
	DWORD wait=WaitForSingleObject(lock,0);
	if(wait!=WAIT_TIMEOUT) {
		if(wait==WAIT_OBJECT_0 || wait==WAIT_ABANDONED) ReleaseMutex(lock);
		goto done;
	}
	if(!vx_path(path,c.state,L"host.lock")) goto done;
	lease=vx_file(path,FALSE,GENERIC_READ|GENERIC_WRITE,0,OPEN_EXISTING);
	ok=lease==INVALID_HANDLE_VALUE && GetLastError()==ERROR_SHARING_VIOLATION;
done:
	if(lease!=INVALID_HANDLE_VALUE && !CloseHandle(lease)) ok=FALSE;
	if(lock && !CloseHandle(lock)) ok=FALSE;
	if(db && MsiCloseHandle(db)!=ERROR_SUCCESS) ok=FALSE;
	if(!vx_close(&c)) ok=FALSE;
	return ok?ERROR_SUCCESS:vx_error(h,L"Unowned or unsafe upgrade removal refused. Run the verified Setup normally; disconnect Labs and Quit the owned bridge first.");
}

static BOOL equals(MSIHANDLE h,const WCHAR *name,const WCHAR *want)
{
	WCHAR value[MAX_PATH];
	return setup_property(h,name,value,ARRAYSIZE(value)) && !wcscmp(value,want);
}
static BOOL operation(MSIHANDLE h,VX_CONTEXT *c,VX_PENDING *p)
{
	BOOL exists=FALSE; WCHAR expected[39],data[64];
	return vx_binary(c,L"Pending",p,sizeof(*p),&exists) && exists && vx_pending(p) &&
		vx_wide(expected,ARRAYSIZE(expected),p->nonce) &&
		setup_property(h,L"CustomActionData",data,ARRAYSIZE(data)) && !wcscmp(data,expected) &&
		equals(h,L"UserSID",c->sid);
}
__declspec(dllexport) UINT __stdcall VxSetupAuthorize(MSIHANDLE h)
{
	VX_DIAG_SPAN span=setup_diag_session_start(h,"authorize");
	VX_CONTEXT c; VX_RECEIPT receipt; WCHAR owner_hash[65],pipe_name[256],root[MAX_PATH];
	MSIHANDLE db=0,message=0; HANDLE pipe=INVALID_HANDLE_VALUE,process=NULL,image=INVALID_HANDLE_VALUE;
	BOOL ok=FALSE; DWORD pid=0,again=0;
	if(!vx_context(&c)) { setup_diag_session_end(h,span,"MSI",ERROR_INSTALL_FAILURE); return ERROR_INSTALL_FAILURE; }
	setup_diag_token(h,c.user_token);
	if(MsiSetPropertyW(h,L"VX_REMOVE_MODE",L"")!=ERROR_SUCCESS ||
		MsiSetPropertyW(h,L"VX_PRESERVE_MODE",L"")!=ERROR_SUCCESS ||
		MsiGetMode(h,MSIRUNMODE_SCHEDULED) || MsiGetMode(h,MSIRUNMODE_COMMIT) || MsiGetMode(h,MSIRUNMODE_ROLLBACK) ||
		MsiGetMode(h,MSIRUNMODE_ADMIN) || !equals(h,L"ALLUSERS",L"") || !equals(h,L"MSIINSTALLPERUSER",L"") ||
		!equals(h,L"UserSID",c.sid) || !equals(h,L"TRANSFORMS",L"") || !equals(h,L"PATCH",L"") ||
		!equals(h,L"RollbackDisabled",L"") || !equals(h,L"DISABLEROLLBACK",L"") ||
		!equals(h,L"MSIRESTARTMANAGERCONTROL",L"Disable") || !equals(h,L"REBOOT",L"ReallySuppress") ||
		!setup_property(h,L"INSTALLDIR",root,ARRAYSIZE(root))) goto done;
	size_t n=wcslen(root); if(n && root[n-1]==L'\\') root[n-1]=0;
	if(_wcsicmp(root,c.root) || !(db=MsiGetActiveDatabase(h)) || !setup_receipt(db,&receipt,owner_hash)) goto done;
	WCHAR revision[41];
	if(!vx_wide(revision,41,receipt.revision) || wcscmp(revision,callback_revision)) goto done;
	if(!equals(h,L"UPGRADINGPRODUCTCODE",L"")) {
		BOOL closed=MsiCloseHandle(db)==ERROR_SUCCESS; db=0;
		if(!vx_close(&c)) closed=FALSE;
		UINT code=closed?setup_upgrade_authorize(h):ERROR_INSTALL_FAILURE;
		setup_diag_session_end(h,span,"MSI",code); return code;
	}
	if(!equals(h,L"ParentProductCode",L"") || !equals(h,L"ParentOriginalDatabase",L"")) goto done;
	if(equals(h,L"REMOVE",L"ALL") && equals(h,L"VX_SETUP_PIPE",L"")) {
		BOOL closed=MsiCloseHandle(db)==ERROR_SUCCESS; db=0;
		if(!vx_close(&c)) closed=FALSE;
		UINT code=closed?setup_remove_authorize(h):ERROR_INSTALL_FAILURE;
		setup_diag_session_end(h,span,"MSI",code); return code;
	}
	if(!setup_support_shortcut(&c,FALSE)) goto done;
	if(!setup_property(h,L"VX_SETUP_PIPE",pipe_name,ARRAYSIZE(pipe_name)) ||
		wcsncmp(pipe_name,VX_SETUP_PIPE_PREFIX,wcslen(VX_SETUP_PIPE_PREFIX))) goto done;
	pipe=CreateFileW(pipe_name,GENERIC_READ|GENERIC_WRITE,0,NULL,OPEN_EXISTING,0,NULL);
	if(pipe==INVALID_HANDLE_VALUE) goto done;
	DWORD mode=PIPE_READMODE_BYTE|PIPE_NOWAIT;
	if(!SetNamedPipeHandleState(pipe,&mode,NULL,NULL) || !GetNamedPipeServerProcessId(pipe,&pid) || !pid) goto done;
	process=OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION|SYNCHRONIZE,FALSE,pid);
	WCHAR path[MAX_PATH]; DWORD length=ARRAYSIZE(path);
	if(!process || !setup_user(process,c.sid) || !QueryFullProcessImageNameW(process,0,path,&length)) goto done;
	image=vx_file(path,FALSE,GENERIC_READ,FILE_SHARE_READ,OPEN_EXISTING);
	char hash[65]; WCHAR wide[65];
	if(image==INVALID_HANDLE_VALUE || !vx_hash(image,hash) || !vx_wide(wide,ARRAYSIZE(wide),hash) ||
		wcscmp(wide,owner_hash) || !GetNamedPipeServerProcessId(pipe,&again) || again!=pid ||
		WaitForSingleObject(process,0)!=WAIT_TIMEOUT) goto done;
	VX_AUTH auth={0}; GUID nonce;
	if(CoCreateGuid(&nonce)!=S_OK || StringFromGUID2(&nonce,auth.nonce,39)!=39 ||
		!vx_wide(auth.product,39,receipt.product) || !vx_wide(auth.revision,41,receipt.revision)) goto done;
	message=MsiCreateRecord(3);
	if(!message || MsiRecordSetStringW(message,0,L"VX_SETUP_AUTH [1] [2] [3]")!=ERROR_SUCCESS ||
		MsiRecordSetStringW(message,1,auth.nonce)!=ERROR_SUCCESS ||
		MsiRecordSetStringW(message,2,auth.product)!=ERROR_SUCCESS || MsiRecordSetStringW(message,3,auth.revision)!=ERROR_SUCCESS ||
		MsiProcessMessage(h,INSTALLMESSAGE_INFO,message)<0 || !setup_transfer(pipe,TRUE,&auth,sizeof(auth))) goto done;
	DWORD accepted=0;
	if(!setup_transfer(pipe,FALSE,&accepted,sizeof(accepted)) || accepted!=1) goto done;
	if(!setup_transfer(pipe,TRUE,&accepted,sizeof(accepted))) goto done;
	/* Only the actual owning API's record handler can admit this challenge. */
	if(!equals(h,L"VX_SETUP_OPERATION",L"")) {
		WCHAR operation_nonce[39];
		if(!setup_property(h,L"VX_SETUP_OPERATION",operation_nonce,ARRAYSIZE(operation_nonce))) goto done;
		VX_PENDING pending; BOOL exists=FALSE; WCHAR target[39];
		if(!vx_open_state(&c,FALSE) || !vx_binary(&c,L"Pending",&pending,sizeof(pending),&exists) ||
			!exists || !vx_pending(&pending) || pending.phase!=VX_MUTATING ||
			memcmp(&pending.after,&receipt,sizeof(receipt)) ||
			!vx_wide(target,39,pending.nonce) || wcscmp(target,operation_nonce)) goto done;
		const WCHAR *names[]={L"VxSetupStartup",L"VxSetupUndoStartup"};
		for(unsigned int i=0;i<ARRAYSIZE(names);++i)
			if(MsiSetPropertyW(h,names[i],operation_nonce)!=ERROR_SUCCESS) goto done;
		ok=TRUE;
	}
done:
	if(message && MsiCloseHandle(message)!=ERROR_SUCCESS) ok=FALSE;
	if(db && MsiCloseHandle(db)!=ERROR_SUCCESS) ok=FALSE;
	if(image!=INVALID_HANDLE_VALUE && !CloseHandle(image)) ok=FALSE;
	if(process && !CloseHandle(process)) ok=FALSE;
	if(pipe!=INVALID_HANDLE_VALUE && !CloseHandle(pipe)) ok=FALSE;
	if(!vx_close(&c)) ok=FALSE;
	setup_diag_session_end(h,span,"MSI",ok?ERROR_SUCCESS:ERROR_INSTALL_FAILURE);
	return ok?ERROR_SUCCESS:vx_error(h,L"Run Setup to install, Repair / Reinstall, or remove this per-user bridge. Raw or unowned MSI operations are refused before changing the bridge.");
}
static UINT startup_action(MSIHANDLE h,BOOL rollback)
{
	VX_DIAG_SPAN span=setup_diag_session_start(h,rollback?"startup_undo":"startup_forward");
	VX_CONTEXT c; VX_PENDING p; BOOL ok=FALSE,held=FALSE;
	if(!MsiGetMode(h,rollback?MSIRUNMODE_ROLLBACK:MSIRUNMODE_SCHEDULED) || !vx_context(&c)) {
		setup_diag_session_end(h,span,"MSI",ERROR_INSTALL_FAILURE); return ERROR_INSTALL_FAILURE;
	}
	if(!vx_enter(&c)) goto done;
	held=TRUE;
	if(!vx_open_state(&c,FALSE) || !operation(h,&c,&p)) goto done;
	/* The original command/absence was journaled before permission to mutate.
	 * run_changed carries the explicitly selected new boolean, not authority.
	 */
	ok=setup_startup(&c,&p,p.run_changed,rollback,FALSE);
done:
	if(held && !vx_leave(&c)) ok=FALSE;
	if(!vx_close(&c)) ok=FALSE;
	setup_diag_session_end(h,span,"MSI",ok?ERROR_SUCCESS:ERROR_INSTALL_FAILURE);
	return ok?ERROR_SUCCESS:vx_error(h,L"Auto-launch on Windows Startup could not be changed/restored with exact ownership. Repair required; no foreign Run entry was taken over.");
}
__declspec(dllexport) UINT __stdcall VxSetupStartup(MSIHANDLE h) { return startup_action(h,FALSE); }
__declspec(dllexport) UINT __stdcall VxSetupUndoStartup(MSIHANDLE h) { return startup_action(h,TRUE); }
__declspec(dllexport) UINT __stdcall VxSetupPreFinalize(MSIHANDLE h)
{
	if(equals(h,L"VX_REMOVE_MODE",L"1")) return setup_remove_verify(h);
	if(MsiGetMode(h,MSIRUNMODE_REBOOTATEND) || MsiGetMode(h,MSIRUNMODE_REBOOTNOW))
		return vx_error(h,L"Setup refuses reboot/deferred replacement. Close the owned bridge normally, then Repair / Reinstall.");
	return ERROR_SUCCESS;
}
