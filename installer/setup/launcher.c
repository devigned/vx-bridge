/* SPDX-License-Identifier: MIT */
#include "protocol.h"
#include <shellapi.h>
#include <shlobj.h>
#include <sddl.h>
#include <objbase.h>
#include <stdio.h>

static CRITICAL_SECTION auth_lock;
static VX_AUTH expected;
static BOOL challenge_ready,api_active;
static volatile LONG ui_info,ui_three,ui_format,ui_target,ui_admitted;
static volatile LONG auth_read,auth_impersonated,auth_context,auth_sid,auth_match,auth_accepted,auth_confirmed;
static DWORD auth_error;
static DWORD ui_record_fields,ui_field0_code,ui_field0_length,ui_field_codes[3];
static char ui_field0_hex[1025];
static DWORD auth_token_error,auth_token_level;
static volatile LONG cancel_requested;
static VX_CONTEXT context;
static VX_RECEIPT target;
static VX_RECEIPT call_target;

typedef struct { HANDLE process; ULONGLONG start; } QUIT_WAIT;
static HRESULT CALLBACK quit_progress(HWND window,UINT notice,WPARAM first,LPARAM second,LONG_PTR data)
{
	(void)first; (void)second;
	QUIT_WAIT *wait=(QUIT_WAIT *)data;
	if(notice==TDN_CREATED) SendMessageW(window,TDM_ENABLE_BUTTON,IDOK,FALSE);
	if(notice==TDN_TIMER) {
		if(WaitForSingleObject(wait->process,0)==WAIT_OBJECT_0) {
			SendMessageW(window,TDM_ENABLE_BUTTON,IDOK,TRUE);
			SendMessageW(window,TDM_CLICK_BUTTON,IDOK,0);
		} else if(GetTickCount64()-wait->start>=120000) SendMessageW(window,TDM_CLICK_BUTTON,IDCANCEL,0);
	}
	return S_OK;
}
static BOOL wait_quit(HANDLE process,BOOL quiet)
{
	DWORD wait=WaitForSingleObject(process,quiet?3000:1000);
	if(wait==WAIT_OBJECT_0) return TRUE;
	if(quiet || wait!=WAIT_TIMEOUT) return FALSE;
	TASKDIALOG_BUTTON buttons[]={{IDOK,L"Continue"},{IDCANCEL,L"Cancel setup"}};
	QUIT_WAIT state={process,GetTickCount64()}; TASKDIALOGCONFIG cfg={0}; int selected=0;
	cfg.cbSize=sizeof(cfg); cfg.dwFlags=TDF_ALLOW_DIALOG_CANCELLATION|TDF_CALLBACK_TIMER|TDF_SIZE_TO_CONTENT;
	cfg.pszWindowTitle=L"VX Bridge Setup";
	cfg.pszMainInstruction=L"Disconnect in ProTee Labs to continue";
	cfg.pszContent=L"Quit was requested safely. Disconnect normally in ProTee Labs; the bridge stays alive until normal departure. Cancel cannot withdraw an already queued Quit. No process will be force-closed.";
	cfg.cButtons=2; cfg.pButtons=buttons; cfg.nDefaultButton=IDCANCEL;
	cfg.pfCallback=quit_progress; cfg.lpCallbackData=(LONG_PTR)&state;
	return TaskDialogIndirect(&cfg,&selected,NULL,NULL)==S_OK && selected==IDOK &&
		WaitForSingleObject(process,0)==WAIT_OBJECT_0;
}

static int dialog(const WCHAR *title,const WCHAR *text,const TASKDIALOG_BUTTON *buttons,UINT count,int initial,BOOL *check)
{
	TASKDIALOGCONFIG cfg={0}; int selected=0;
	cfg.cbSize=sizeof(cfg); cfg.dwFlags=TDF_ALLOW_DIALOG_CANCELLATION|TDF_SIZE_TO_CONTENT;
	cfg.pszWindowTitle=L"VX Bridge Setup"; cfg.pszMainInstruction=title; cfg.pszContent=text;
	cfg.cButtons=count; cfg.pButtons=buttons; cfg.nDefaultButton=initial;
	if(check) { cfg.pszVerificationText=VX_AUTOLAUNCH_LABEL; if(*check) cfg.dwFlags|=TDF_VERIFICATION_FLAG_CHECKED; }
	if(TaskDialogIndirect(&cfg,&selected,NULL,check)!=S_OK) return 0;
	return selected;
}
static void failure(const WCHAR *text)
{
	TASKDIALOG_BUTTON button={IDCLOSE,L"Close"};
	dialog(L"Setup did not complete",text,&button,1,IDCLOSE,NULL);
}
static int CALLBACK installer_ui(void *unused,UINT kind,MSIHANDLE record)
{
	(void)unused;
	if(InterlockedCompareExchange(&cancel_requested,0,0)) return IDCANCEL;
	if((kind&0xff000000)==INSTALLMESSAGE_INFO) {
		InterlockedIncrement(&ui_info);
		WCHAR value[256]={0}; DWORD length=ARRAYSIZE(value);
		UINT code=MsiRecordGetStringW(record,0,value,&length);
		if((code==ERROR_SUCCESS || code==ERROR_MORE_DATA) && !wcsncmp(value,L"VX_SETUP_AUTH ",14)) {
			ui_record_fields=MsiRecordGetFieldCount(record);
			ui_field0_code=code; ui_field0_length=length;
			const char digits[]="0123456789abcdef";
			size_t count=wcslen(value);
			for(size_t i=0;i<count && i<255;++i) {
				unsigned int v=value[i];
				for(unsigned int j=0;j<4;++j) ui_field0_hex[i*4+j]=digits[(v>>(12-j*4))&15];
			}
			for(unsigned int i=0;i<3;++i) {
				WCHAR field[64]; DWORD n=ARRAYSIZE(field);
				ui_field_codes[i]=MsiRecordGetStringW(record,i+1,field,&n);
			}
		}
	}
	if((kind&0xff000000)==INSTALLMESSAGE_INFO && MsiRecordGetFieldCount(record)==3) {
		InterlockedIncrement(&ui_three);
		VX_AUTH candidate={0}; WCHAR format[96]; DWORD n=ARRAYSIZE(format);
		if(MsiRecordGetStringW(record,0,format,&n)==ERROR_SUCCESS && !wcscmp(format,L"VX_SETUP_AUTH [1] [2] [3]")) {
			InterlockedIncrement(&ui_format);
			DWORD a=39,b=39,c=41;
			if(MsiRecordGetStringW(record,1,candidate.nonce,&a)==ERROR_SUCCESS &&
				MsiRecordGetStringW(record,2,candidate.product,&b)==ERROR_SUCCESS &&
				MsiRecordGetStringW(record,3,candidate.revision,&c)==ERROR_SUCCESS) {
				WCHAR product[39],revision[41];
				if(vx_wide(product,39,call_target.product) && vx_wide(revision,41,call_target.revision) &&
					!wcscmp(candidate.product,product) && !wcscmp(candidate.revision,revision)) {
					InterlockedIncrement(&ui_target);
					EnterCriticalSection(&auth_lock);
					if(api_active && !challenge_ready) { expected=candidate; challenge_ready=TRUE; InterlockedIncrement(&ui_admitted); }
					LeaveCriticalSection(&auth_lock);
				}
			}
		}
	}
	if((kind&0xff000000)==INSTALLMESSAGE_FILESINUSE || (kind&0xff000000)==INSTALLMESSAGE_RMFILESINUSE) return IDCANCEL;
	return 0;
}
typedef struct { const WCHAR *path,*parameters; HANDLE done; UINT code; } MSI_CALL;
static DWORD WINAPI install_worker(void *arg)
{
	MSI_CALL *call=arg;
	call->code=MsiInstallProductW(call->path,call->parameters);
	SetEvent(call->done);
	return 0;
}
static HRESULT CALLBACK install_progress(HWND window,UINT notice,WPARAM first,LPARAM second,LONG_PTR data)
{
	(void)second;
	MSI_CALL *call=(MSI_CALL *)data;
	if(notice==TDN_CREATED) SendMessageW(window,TDM_ENABLE_BUTTON,IDOK,FALSE);
	if(notice==TDN_TIMER && WaitForSingleObject(call->done,0)==WAIT_OBJECT_0) {
		SendMessageW(window,TDM_ENABLE_BUTTON,IDOK,TRUE);
		SendMessageW(window,TDM_CLICK_BUTTON,IDOK,0);
	}
	if(notice==TDN_BUTTON_CLICKED && first==IDCANCEL) {
		InterlockedExchange(&cancel_requested,1);
		SendMessageW(window,TDM_SET_ELEMENT_TEXT,TDE_CONTENT,
			(LPARAM)L"Cancellation requested. Waiting for Windows Installer to return safely; no process will be force-closed. Do not start another setup operation.");
		return S_FALSE;
	}
	return S_OK;
}
static UINT install_package(const WCHAR *path,const WCHAR *parameters,BOOL quiet)
{
	if(quiet) return MsiInstallProductW(path,parameters);
	MSI_CALL call={path,parameters,NULL,ERROR_INSTALL_FAILURE};
	call.done=CreateEventW(NULL,TRUE,FALSE,NULL);
	if(!call.done) return ERROR_INSTALL_FAILURE;
	HANDLE thread=CreateThread(NULL,0,install_worker,&call,0,NULL);
	if(!thread) { CloseHandle(call.done); return ERROR_INSTALL_FAILURE; }
	TASKDIALOG_BUTTON buttons[]={{IDOK,L"Continue"},{IDCANCEL,L"Cancel"}};
	TASKDIALOGCONFIG cfg={0}; int selected=0;
	cfg.cbSize=sizeof(cfg); cfg.dwFlags=TDF_ALLOW_DIALOG_CANCELLATION|TDF_CALLBACK_TIMER|TDF_SHOW_MARQUEE_PROGRESS_BAR;
	cfg.pszWindowTitle=L"VX Bridge Setup";
	cfg.pszMainInstruction=L"Windows Installer is applying the owned package";
	cfg.pszContent=L"Cancel requests normal installer rollback. Setup never force-closes the bridge or Windows Installer.";
	cfg.cButtons=2; cfg.pButtons=buttons; cfg.nDefaultButton=IDCANCEL;
	cfg.pfCallback=install_progress; cfg.lpCallbackData=(LONG_PTR)&call;
	HRESULT ui=TaskDialogIndirect(&cfg,&selected,NULL,NULL);
	if(ui!=S_OK) InterlockedExchange(&cancel_requested,1);
	/* No timeout can turn an active synchronous engine call into a safe return. */
	WaitForSingleObject(thread,INFINITE);
	BOOL closed=CloseHandle(thread);
	if(!CloseHandle(call.done)) closed=FALSE;
	return ui==S_OK && closed?call.code:ERROR_INSTALL_FAILURE;
}
typedef struct { HANDLE pipe,stop; } PIPE_WORK;
static DWORD WINAPI auth_worker(void *arg)
{
	PIPE_WORK *w=arg;
	while(WaitForSingleObject(w->stop,0)==WAIT_TIMEOUT) {
		BOOL connected=ConnectNamedPipe(w->pipe,NULL);
		DWORD e=GetLastError();
		if(!connected && e!=ERROR_PIPE_CONNECTED) { Sleep(10); continue; }
		VX_AUTH actual={0}; DWORD accepted=0,pid=0;
		if(GetNamedPipeClientProcessId(w->pipe,&pid) && pid && setup_transfer(w->pipe,FALSE,&actual,sizeof(actual))) {
			InterlockedIncrement(&auth_read);
			/* Authenticate the effective pipe client, not the MSI service's process token. */
			if(ImpersonateNamedPipeClient(w->pipe)) {
				setup_diag_event(0,"pipe_impersonation","Win32",ERROR_SUCCESS);
				InterlockedIncrement(&auth_impersonated);
				HANDLE diagnostic=NULL; DWORD bytes=0;
				if(OpenThreadToken(GetCurrentThread(),TOKEN_QUERY,TRUE,&diagnostic)) {
					if(!GetTokenInformation(diagnostic,TokenImpersonationLevel,&auth_token_level,sizeof(auth_token_level),&bytes))
						auth_token_error=GetLastError();
					CloseHandle(diagnostic);
				} else auth_token_error=GetLastError();
				VX_CONTEXT client;
				BOOL user=vx_context(&client);
				setup_diag_event(0,"pipe_effective_context","check",user?0:1);
				if(user) {
					InterlockedIncrement(&auth_context);
					user=!wcscmp(client.sid,context.sid);
					setup_diag_event(0,"pipe_effective_sid_match","check",user?0:1);
					if(user) InterlockedIncrement(&auth_sid);
					vx_close(&client);
				} else auth_error=GetLastError();
				if(!RevertToSelf()) return 1;
				EnterCriticalSection(&auth_lock);
				if(challenge_ready && !memcmp(&actual,&expected,sizeof(actual))) InterlockedIncrement(&auth_match);
				if(user && api_active && challenge_ready && !memcmp(&actual,&expected,sizeof(actual))) {
					accepted=1; challenge_ready=FALSE;
					InterlockedIncrement(&auth_accepted);
				} else auth_error=GetLastError();
				LeaveCriticalSection(&auth_lock);
			}
		}
		if(!setup_transfer(w->pipe,TRUE,&accepted,sizeof(accepted))) return 1;
		if(accepted) {
			/* DisconnectNamedPipe discards unread replies; wait for consumption,
			 * without FlushFileBuffers' unbounded client-dependent wait.
			 */
			DWORD confirmed=0;
			if(!setup_transfer(w->pipe,FALSE,&confirmed,sizeof(confirmed)) || confirmed!=1) return 1;
			InterlockedIncrement(&auth_confirmed);
		}
		if(!DisconnectNamedPipe(w->pipe)) return 1;
	}
	return 0;
}
static BOOL owner_quit_wait(HANDLE process,void *data) { return wait_quit(process,*(BOOL *)data); }
static BOOL owned_quit(const VX_RECEIPT *before,BOOL quiet)
{
	return setup_owned_quit(&context,before,owner_quit_wait,&quiet);
}
static BOOL registered(const VX_RECEIPT *r,BOOL absent)
{
	VX_DIAG_SPAN span=setup_diag_start(absent?"product_absence_check":"product_registered_check");
	BOOL ok=vx_registered(&context,r,absent);
	setup_diag_end(span,"check",ok?0:1); return ok;
}
static BOOL existing_registry(void)
{
	HKEY user=NULL;
	if(RegOpenCurrentUser(KEY_READ|KEY_WRITE,&user)!=ERROR_SUCCESS) return FALSE;
	LSTATUS status=RegOpenKeyExW(user,VX_STATE_KEY,0,KEY_READ|KEY_WRITE,&context.key);
	if(status==ERROR_FILE_NOT_FOUND) context.key=NULL;
	BOOL ok=status==ERROR_SUCCESS || status==ERROR_FILE_NOT_FOUND;
	if(RegCloseKey(user)!=ERROR_SUCCESS) ok=FALSE;
	return ok;
}
static BOOL source(MSIHANDLE *db,VX_RECEIPT *r,const WCHAR *path)
{
	WCHAR expected_hash[65],self[MAX_PATH]; char hash[65],wanted[65];
	if(MsiOpenDatabaseW(path,MSIDBOPEN_READONLY,db)!=ERROR_SUCCESS ||
		!setup_receipt(*db,r,expected_hash) || !GetModuleFileNameW(NULL,self,ARRAYSIZE(self))) return FALSE;
	HANDLE pin=vx_file(self,FALSE,GENERIC_READ,FILE_SHARE_READ,OPEN_EXISTING);
	if(pin==INVALID_HANDLE_VALUE) return FALSE;
	BOOL ok=vx_hash(pin,hash) && vx_ascii(wanted,65,expected_hash) && !strcmp(hash,wanted);
	if(!CloseHandle(pin)) ok=FALSE;
	return ok;
}
static BOOL read_source(WCHAR path[MAX_PATH])
{
	DWORD type=0,n=MAX_PATH*sizeof(WCHAR);
	if(context.key && RegQueryValueExW(context.key,L"SourcePackage",NULL,&type,(BYTE *)path,&n)==ERROR_SUCCESS)
		return type==REG_SZ && n>=2 && n<=MAX_PATH*2 && !(n%2) && path[n/2-1]==0 && (wcslen(path)+1)*2==n;
	VX_RECEIPT receipt; BOOL exists=FALSE; WCHAR package[39],name[48];
	return vx_binary(&context,L"Receipt",&receipt,sizeof(receipt),&exists) && exists && vx_receipt(&receipt) &&
		vx_wide(package,39,receipt.package) && swprintf(name,ARRAYSIZE(name),L"%ls.msi",package)>0 &&
		vx_path(path,context.state,name);
}
static BOOL cache_file(const WCHAR *source_path,const WCHAR *destination)
{
	HANDLE input=vx_file(source_path,FALSE,GENERIC_READ,FILE_SHARE_READ,OPEN_EXISTING);
	if(input==INVALID_HANDLE_VALUE) return FALSE;
	char want[65],actual[65]; BOOL ok=vx_hash(input,want);
	if(!CloseHandle(input)) ok=FALSE;
	if(!ok) return FALSE;
	if(_wcsicmp(source_path,destination) && !CopyFileW(source_path,destination,TRUE) && GetLastError()!=ERROR_FILE_EXISTS) return FALSE;
	HANDLE output=vx_file(destination,FALSE,GENERIC_READ,FILE_SHARE_READ,OPEN_EXISTING);
	if(output==INVALID_HANDLE_VALUE) return FALSE;
	ok=vx_hash(output,actual) && !strcmp(want,actual);
	if(!CloseHandle(output)) ok=FALSE;
	return ok;
}
static BOOL root_fresh(void)
{
	WCHAR pattern[MAX_PATH]; WIN32_FIND_DATAW entry;
	if(!setup_root_pinned(&context)) return FALSE;
	if(!vx_path(pattern,context.root,L"*")) return FALSE;
	HANDLE find=FindFirstFileW(pattern,&entry);
	if(find==INVALID_HANDLE_VALUE) return GetLastError()==ERROR_FILE_NOT_FOUND || GetLastError()==ERROR_PATH_NOT_FOUND;
	BOOL empty=TRUE;
	do { if(wcscmp(entry.cFileName,L".") && wcscmp(entry.cFileName,L"..")) { empty=FALSE; break; } }
	while(FindNextFileW(find,&entry));
	if(empty && GetLastError()!=ERROR_NO_MORE_FILES) empty=FALSE;
	if(!FindClose(find)) empty=FALSE;
	return empty;
}
static BOOL result_log(UINT install,BOOL usable,int result)
{
	WCHAR path[MAX_PATH];
	if(!vx_path(path,context.state,L"setup-result.log")) return FALSE;
	HANDLE file=vx_file(path,FALSE,FILE_APPEND_DATA,FILE_SHARE_READ,OPEN_ALWAYS);
	if(file==INVALID_HANDLE_VALUE) return FALSE;
	LARGE_INTEGER size; char line[2048]; DWORD wrote=0;
	int n=snprintf(line,sizeof(line),"contract=recovery-first-v2 install_called=%u install=%u engine_namespace=conventional usable=%u result=%d source=%s ui_info=%ld ui_three=%ld ui_format=%ld ui_target=%ld ui_admitted=%ld auth_read=%ld auth_impersonated=%ld auth_context=%ld auth_sid=%ld auth_match=%ld auth_accepted=%ld auth_error=%lu ui_record_fields=%lu ui_field0_code=%lu ui_field0_length=%lu ui_field1_code=%lu ui_field2_code=%lu ui_field3_code=%lu ui_field0_hex=%s auth_token_error=%lu auth_token_level=%lu auth_confirmed=%ld\r\n",
		install!=(UINT)-1,install,usable?1U:0U,result,target.revision,
		(long)ui_info,(long)ui_three,(long)ui_format,(long)ui_target,(long)ui_admitted,
		(long)auth_read,(long)auth_impersonated,(long)auth_context,(long)auth_sid,(long)auth_match,(long)auth_accepted,(unsigned long)auth_error,
		(unsigned long)ui_record_fields,(unsigned long)ui_field0_code,(unsigned long)ui_field0_length,
		(unsigned long)ui_field_codes[0],(unsigned long)ui_field_codes[1],(unsigned long)ui_field_codes[2],
		ui_field0_hex[0]?ui_field0_hex:"none",(unsigned long)auth_token_error,(unsigned long)auth_token_level,(long)auth_confirmed);
	BOOL ok=GetFileSizeEx(file,&size) && size.QuadPart<256*1024 && n>0 && n<(int)sizeof(line) &&
		WriteFile(file,line,(DWORD)n,&wrote,NULL) && wrote==(DWORD)n && FlushFileBuffers(file);
	if(!CloseHandle(file)) ok=FALSE;
	return ok;
}
static BOOL start_bridge(void)
{
	WCHAR image[MAX_PATH],command[MAX_PATH+16]; STARTUPINFOW si={0}; PROCESS_INFORMATION pi={0};
	if(!vx_path(image,context.root,L"GSPconnect.exe") || swprintf(command,ARRAYSIZE(command),L"\"%ls\" --desktop",image)<=0) return FALSE;
	si.cb=sizeof(si);
	/* No inheritance, job assignment, elevated token or dependency on Setup exit. */
	if(!CreateProcessW(image,command,NULL,NULL,FALSE,CREATE_NEW_PROCESS_GROUP|CREATE_BREAKAWAY_FROM_JOB,NULL,context.root,&si,&pi)) return FALSE;
	BOOL ok=CloseHandle(pi.hThread);
	if(!CloseHandle(pi.hProcess)) ok=FALSE;
	return ok;
}
static int execute(const WCHAR *input,BOOL quiet)
{
	MSIHANDLE db=0; HANDLE lease=INVALID_HANDLE_VALUE,source_pin=INVALID_HANDLE_VALUE;
	HANDLE owner_lock=NULL,thread=NULL; PIPE_WORK work={INVALID_HANDLE_VALUE,NULL};
	WCHAR path[MAX_PATH],pipe_name[256],nonce[39],parameters[2048],log[MAX_PATH];
	VX_PENDING p={0}; BOOL held=FALSE,old_exists=FALSE,pending_exists=FALSE,ready=FALSE;
	BOOL root_created=FALSE; unsigned int root_slot=(unsigned int)-1;
	BY_HANDLE_FILE_INFORMATION root_before={0};
	UINT install=(UINT)-1;
	int result=1; BOOL enabled=FALSE,choice=FALSE; WCHAR old_command[261];
	const char *stage="context_and_admission";
	if(!vx_context(&context) || !vx_enter(&context)) goto done;
	setup_diag_token(0,context.user_token);
	held=TRUE;
	if(!existing_registry()) goto done;
	if(input) { if(wcslen(input)>=MAX_PATH || !GetFullPathNameW(input,MAX_PATH,path,NULL)) goto done; }
	else if(!read_source(path)) {
		if(!GetModuleFileNameW(NULL,path,ARRAYSIZE(path))) goto done;
		WCHAR *slash=wcsrchr(path,L'\\');
		if(!slash) goto done;
		*slash=0;
		WCHAR adjacent[MAX_PATH];
		if(!vx_path(adjacent,path,L"bridge.msi")) goto done;
		wcscpy(path,adjacent);
	}
	source_pin=vx_file(path,FALSE,GENERIC_READ,FILE_SHARE_READ,OPEN_EXISTING);
	setup_diag_handle("target_source","open",source_pin,GENERIC_READ,FILE_SHARE_READ,OPEN_EXISTING);
	stage="target_source_identity";
	if(source_pin==INVALID_HANDLE_VALUE || !source(&db,&target,path)) goto done;
	if(!setup_pin_root(&context,&root_created,&root_slot)) goto done;
	if(!setup_root_snapshot(&context,root_slot,&root_before)) goto done;
	setup_diag_handle("owned_root","initial-pin",context.pins[root_slot],FILE_READ_ATTRIBUTES,FILE_SHARE_READ,OPEN_EXISTING);
	stage="pending_and_original_receipt"; VX_PENDING stale;
	if(!vx_binary(&context,L"Pending",&stale,sizeof(stale),&pending_exists) ||
		(pending_exists && (!vx_pending(&stale) || memcmp(&stale.after,&target,sizeof(target))))) goto done;
	BOOL recovery=pending_exists;
	if(!vx_binary(&context,L"Receipt",&p.before,sizeof(p.before),&old_exists) ||
		(old_exists && !vx_receipt(&p.before))) goto done;
	/* A missing receipt is recoverable only for this exact registered package. */
	if(!old_exists) {
		if(recovery && !stale.has_before) {
			if(!root_fresh() && !setup_payload(&context,&target,TRUE)) goto done;
		} else if(registered(&target,FALSE)) { p.before=target; old_exists=TRUE; }
		else if(!registered(&target,TRUE) || (!root_fresh() && !(recovery && setup_payload(&context,&target,TRUE)))) goto done;
	}
	if(old_exists) {
		if(strcmp(p.before.revision,target.revision)) goto done;
		if(!recovery && !registered(&p.before,FALSE)) goto done;
		if(recovery && (!stale.has_before || memcmp(&p.before,&stale.before,sizeof(p.before)))) goto done;
		unsigned int a,b,c,x,y,z;
		if(sscanf(p.before.version,"%u.%u.%u",&a,&b,&c)!=3 ||
			sscanf(target.version,"%u.%u.%u",&x,&y,&z)!=3 ||
			x<a || (x==a && y<b) || (x==a && y==b && z<c) ||
			(!recovery && x==a && y==b && z==c && memcmp(&p.before,&target,sizeof(target)))) goto done;
	}
	BOOL installed=!registered(&target,TRUE);
	if(installed && !setup_catalog_owned(&context,old_exists?&p.before:&target,&target)) goto done;
	const WCHAR *maintenance=L"ADDLOCAL=Bridge REINSTALLMODE=amus";
	if(installed) {
		WCHAR product[39]; INSTALLSTATE feature=INSTALLSTATE_UNKNOWN;
		if(!vx_wide(product,39,target.product) ||
			MsiQueryFeatureStateExW(product,context.sid,MSIINSTALLCONTEXT_USERUNMANAGED,L"Bridge",&feature)!=ERROR_SUCCESS) goto done;
		if(feature==INSTALLSTATE_LOCAL || feature==INSTALLSTATE_SOURCE)
			maintenance=L"REINSTALL=ALL REINSTALLMODE=amus";
		else if(feature==INSTALLSTATE_ADVERTISED || feature==INSTALLSTATE_ABSENT)
			maintenance=L"ADDLOCAL=Bridge REINSTALLMODE=amus";
		else goto done;
	}
	if(old_exists && !recovery && strcmp(p.before.product,target.product)) {
		MSIHANDLE old_db=0; VX_RECEIPT old_identity; WCHAR old_path[MAX_PATH];
		if(!read_source(old_path) || !source(&old_db,&old_identity,old_path)) {
			if(old_db) MsiCloseHandle(old_db);
			goto done;
		}
		BOOL same=!memcmp(&old_identity,&p.before,sizeof(p.before));
		if(MsiCloseHandle(old_db)!=ERROR_SUCCESS) same=FALSE;
		if(!same) goto done;
	}
	if(!setup_related_owned(&context,old_exists?&p.before:NULL,&target)) goto done;
	stage="owned_startup_and_owner_lock";
	if(!setup_startup_state(&context,&enabled,old_command) || (!old_exists && !recovery && enabled)) goto done;
	WCHAR lock_name[320];
	if(swprintf(lock_name,ARRAYSIZE(lock_name),L"Global\\VxBridge.SetupOwner.%ls",context.sid)<=0) goto done;
	owner_lock=CreateMutexW(NULL,FALSE,lock_name);
	if(!owner_lock || WaitForSingleObject(owner_lock,0)!=WAIT_OBJECT_0) goto done;
	if(context.key) {
		if(RegCloseKey(context.key)!=ERROR_SUCCESS) goto done;
		context.key=NULL;
	}
	if(!vx_open_state(&context,TRUE)) goto done;
	if(!vx_leave(&context)) goto done;
	held=FALSE;
	choice=enabled;
	if(!quiet) {
		TASKDIALOG_BUTTON buttons[]={{IDYES,old_exists||recovery?L"Repair / Reinstall":L"Install"},{IDCANCEL,L"Cancel"}};
		if(dialog(old_exists||recovery?L"Repair / Reinstall the bridge":L"Install the bridge",
			L"Per-user installation. No administrator access, automatic app start, vendor changes, or forced shutdown.\n\n" VX_AUTOLAUNCH_HELP,
			buttons,2,IDYES,&choice)!=IDYES) { result=2; goto done; }
	}
	WCHAR cached_source[MAX_PATH],cached_setup[MAX_PATH],self[MAX_PATH],cache_name[48];
	stage="target_passive_cache";
	WCHAR package[39];
	if(!vx_wide(package,39,target.package) ||
		swprintf(cache_name,ARRAYSIZE(cache_name),L"%ls.msi",package)<=0 ||
		!vx_path(cached_source,context.state,cache_name) || !vx_path(cached_setup,context.state,L"setup.exe") ||
		!GetModuleFileNameW(NULL,self,ARRAYSIZE(self)) || !cache_file(path,cached_source) ||
		!cache_file(self,cached_setup) || !setup_cache_record(&context,cache_name,cached_source) ||
		!setup_cache_record(&context,L"setup.exe",cached_setup)) goto done;
	stage="pending_publication";
	if(!vx_enter(&context)) goto done;
	held=TRUE;
	memcpy(p.magic,"VXTX001",8); p.size=sizeof(p); p.phase=VX_WAITING;
	p.operation=old_exists?VX_REPAIR:VX_INSTALL; p.after=target; p.has_before=old_exists;
	GUID id;
	if(CoCreateGuid(&id)!=S_OK || StringFromGUID2(&id,nonce,39)!=39 || !vx_ascii(p.nonce,39,nonce) ||
		!setup_startup(&context,&p,0,FALSE,TRUE)) goto done;
	p.run_changed=choice;
	if(!vx_pending(&p) || !vx_put(&context,L"Pending",&p,sizeof(p))) goto done;
	if(!vx_leave(&context)) goto done;
	held=FALSE;
	stage="source_safe_lease"; lease=setup_lifetime(&context);
	if(lease==INVALID_HANDLE_VALUE) {
		if(!old_exists || !owned_quit(&p.before,quiet) || (lease=setup_lifetime(&context))==INVALID_HANDLE_VALUE) goto done;
	}
	stage="before_payload_inventory";
	if(!old_exists && !root_fresh() && !(recovery && setup_payload(&context,&target,TRUE))) goto done;
	if(old_exists && !setup_payload(&context,&p.before,TRUE)) goto done;
	if(swprintf(pipe_name,ARRAYSIZE(pipe_name),VX_SETUP_PIPE_PREFIX L"%ls.%ls",context.sid,nonce)<=0) goto done;
	WCHAR sddl[640]; PSECURITY_DESCRIPTOR sd=NULL;
	if(swprintf(sddl,ARRAYSIZE(sddl),L"D:P(A;;GA;;;SY)(A;;GA;;;%ls)",context.sid)<=0 ||
		!ConvertStringSecurityDescriptorToSecurityDescriptorW(sddl,SDDL_REVISION_1,&sd,NULL)) goto done;
	SECURITY_ATTRIBUTES sa={sizeof(sa),sd,FALSE};
	work.pipe=CreateNamedPipeW(pipe_name,PIPE_ACCESS_DUPLEX|FILE_FLAG_FIRST_PIPE_INSTANCE,
		PIPE_TYPE_BYTE|PIPE_READMODE_BYTE|PIPE_NOWAIT|PIPE_REJECT_REMOTE_CLIENTS,1,4096,4096,0,&sa);
	LocalFree(sd);
	work.stop=CreateEventW(NULL,TRUE,FALSE,NULL);
	if(work.pipe==INVALID_HANDLE_VALUE || !work.stop) goto done;
	thread=CreateThread(NULL,0,auth_worker,&work,0,NULL); if(!thread) goto done;
	stage="external_ui_and_logging";
	if(!vx_path(log,context.state,L"setup-msi.log") ||
		MsiSetInternalUI(INSTALLUILEVEL_NONE,NULL)==INSTALLUILEVEL_NOCHANGE ||
		MsiSetExternalUIRecord(installer_ui,INSTALLLOGMODE_INFO|INSTALLLOGMODE_PROGRESS|INSTALLLOGMODE_ERROR|INSTALLLOGMODE_FILESINUSE|INSTALLLOGMODE_RMFILESINUSE,NULL,NULL)!=ERROR_SUCCESS ||
		MsiEnableLogW(INSTALLLOGMODE_VERBOSE|INSTALLLOGMODE_INFO|INSTALLLOGMODE_ERROR|INSTALLLOGMODE_ACTIONSTART|
			(setup_diagnostics_enabled()?INSTALLLOGMODE_EXTRADEBUG:0),log,
			INSTALLLOGATTRIBUTES_APPEND|INSTALLLOGATTRIBUTES_FLUSHEACHLINE)!=ERROR_SUCCESS) goto done;
	p.phase=VX_MUTATING;
	if(!vx_put(&context,L"Pending",&p,sizeof(p))) goto done;
	if(swprintf(parameters,ARRAYSIZE(parameters),
		L"VX_SETUP_PIPE=\"%ls\" VX_SETUP_OPERATION=\"%ls\" VX_CACHE_SOURCE=\"%ls\" VX_MAINTENANCE_EXE=\"%ls\" REBOOT=ReallySuppress %ls",
		pipe_name,nonce,cached_source,cached_setup,
		maintenance)<=0) goto done;
	if(!setup_release_root(&context,&root_slot,&root_before)) goto done;
	stage="target_install";
	EnterCriticalSection(&auth_lock); call_target=target; api_active=TRUE; LeaveCriticalSection(&auth_lock);
	VX_DIAG_SPAN engine_span=setup_diag_start("target_install");
	install=install_package(path,parameters,quiet);
	setup_diag_end(engine_span,"MSI",install);
	EnterCriticalSection(&auth_lock); api_active=FALSE; LeaveCriticalSection(&auth_lock);
	if(install!=ERROR_SUCCESS) goto done;
	stage="terminal_product_and_payload";
	if(!setup_repin_root(&context,&root_slot,NULL) ||
		!registered(&target,FALSE) || !setup_single_product(&target) || !setup_payload(&context,&target,FALSE)) goto done;
	if(!vx_enter(&context)) goto done;
	held=TRUE;
	stage="receipt_publication"; engine_span=setup_diag_start("receipt_publication");
	BOOL receipt_written=vx_put(&context,L"Receipt",&target,sizeof(target));
	setup_diag_end(engine_span,"check",receipt_written?0:1);
	if(!receipt_written) goto done;
	stage="pending_removal"; engine_span=setup_diag_start("pending_removal");
	BOOL pending_removed=vx_delete(&context,L"Pending");
	setup_diag_end(engine_span,"check",pending_removed?0:1);
	if(!pending_removed) goto done;
	ready=TRUE; result=0;
	goto done;
done:
	if(result) setup_diag_event(0,stage,"incomplete_stage",1);
	if(root_created && root_slot!=(unsigned int)-1 &&
		install==(UINT)-1 && !ready && root_fresh() &&
		!setup_remove_root(&context,&root_slot)) result=1;
	/* Failed MSI or unverified postconditions retain Pending for explicit maintenance. */
	if(thread) {
		SetEvent(work.stop);
		if(WaitForSingleObject(thread,5000)!=WAIT_OBJECT_0) {
			/* Stack/context must remain alive if the authentication thread did not return. */
			failure(L"Setup authentication did not stop. Repair required. No bridge was launched.");
			ExitProcess(1);
		}
		DWORD code=1; if(!GetExitCodeThread(thread,&code) || code || !CloseHandle(thread)) result=1;
	}
	if(work.pipe!=INVALID_HANDLE_VALUE && !CloseHandle(work.pipe)) result=1;
	if(work.stop && !CloseHandle(work.stop)) result=1;
	if(db && MsiCloseHandle(db)!=ERROR_SUCCESS) result=1;
	if(source_pin!=INVALID_HANDLE_VALUE && !CloseHandle(source_pin)) result=1;
	if(held && !vx_leave(&context)) result=1;
	if(lease!=INVALID_HANDLE_VALUE && !CloseHandle(lease)) result=1;
	if(owner_lock) {
		if(!ReleaseMutex(owner_lock)) result=1;
		if(!CloseHandle(owner_lock)) result=1;
	}
	if(!vx_close(&context)) result=1;
	if(context.state[0] && !result_log(install,ready && result==0,result)) {
		result=1;
		if(!quiet) failure(L"Setup result could not be recorded. Maintenance completion is unverified; run Setup and choose Repair / Reinstall. No bridge was launched.");
	}
	if(result) {
		if(!quiet) failure(L"Setup or maintenance did not complete. Repair required: run the verified Setup package and choose Repair / Reinstall. If Repair cannot complete, fully uninstall the recorded versions through Windows Installed apps or their verified original MSIs with REMOVE=ALL, then install fresh. Unknown cleanup or remaining registrations are not successful recovery. A failed operation is not guaranteed to restore the previous version.\n\nDisconnect in ProTee Labs and Quit normally first. No forced shutdown, automatic restart or reboot was performed.");
	} else if(ready && !quiet) {
		TASKDIALOG_BUTTON buttons[]={{IDYES,L"Start bridge"},{IDNO,L"Not now"}};
		if(dialog(L"Start the bridge now?",L"Installation succeeded. Starting now does not change Auto-launch on Windows Startup. Connection/game readiness is not implied.",buttons,2,IDNO,NULL)==IDYES &&
			!start_bridge()) {
			TASKDIALOG_BUTTON close={IDCLOSE,L"Close"};
			dialog(L"Installation succeeded; bridge could not start",
				L"Start GSPconnect.exe manually after resolving the runtime conflict. This launch error does not undo the installation.",
				&close,1,IDCLOSE,NULL);
		}
	}
	return result;
}
int WINAPI wWinMain(HINSTANCE instance,HINSTANCE previous,WCHAR *command,int show)
{
	(void)instance; (void)previous; (void)command; (void)show;
	int argc=0; WCHAR **argv=CommandLineToArgvW(GetCommandLineW(),&argc);
	if(!argv) return 1;
	BOOL quiet=FALSE,remove=FALSE; const WCHAR *package=NULL;
	for(int i=1;i<argc;++i) {
		if(!wcscmp(argv[i],L"/quiet")) quiet=TRUE;
		else if(!wcscmp(argv[i],L"/remove")) remove=TRUE;
		else if(!wcscmp(argv[i],L"/maintain")) {}
		else if(!wcscmp(argv[i],L"/package") && i+1<argc) package=argv[++i];
		else { LocalFree(argv); return 2; }
	}
	if(remove) {
		if(!quiet) failure(L"Uninstall VX Bridge through Windows Settings > Apps > Installed apps, or msiexec /x ProductCode. "
			L"Windows Installer owns removal, including this passive Setup cache. This running Setup does not attempt to delete itself. "
			L"No original download is needed for normal uninstall.");
		LocalFree(argv);
		return ERROR_CALL_NOT_IMPLEMENTED;
	}
	TOKEN_ELEVATION elevation; DWORD n=sizeof(elevation); HANDLE token=NULL;
	BOOL user=OpenProcessToken(GetCurrentProcess(),TOKEN_QUERY,&token) &&
		GetTokenInformation(token,TokenElevation,&elevation,n,&n) && !elevation.TokenIsElevated;
	if(token && !CloseHandle(token)) user=FALSE;
	if(!user) {
		LocalFree(argv); failure(L"Run Setup normally as the installing user, not as administrator."); return 1;
	}
	InitializeCriticalSection(&auth_lock);
	int result=execute(package,quiet);
	DeleteCriticalSection(&auth_lock); LocalFree(argv);
	return result;
}
