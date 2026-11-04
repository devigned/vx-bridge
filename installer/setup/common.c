/* SPDX-License-Identifier: MIT */
#include "protocol.h"
#include <sddl.h>
#include <stdlib.h>
#include <objbase.h>
#include <stdio.h>

static LONG diagnostic_sequence;
BOOL setup_diagnostics_enabled(void)
{
	DWORD error=GetLastError(); WCHAR value[2]={0};
	BOOL enabled=GetEnvironmentVariableW(L"VX_SETUP_DIAGNOSTICS",value,2)==1 && value[0]==L'1';
	SetLastError(error); return enabled;
}
static void diagnostic(MSIHANDLE session,const char *event,const char *label,const char *domain,DWORD code,LONG id,ULONGLONG elapsed)
{
	DWORD error=GetLastError();
	SYSTEMTIME utc; GetSystemTime(&utc);
	char line[768];
	int n=snprintf(line,sizeof(line),
		"VX_SETUP_DIAG {\"schema\":1,\"event\":\"%s\",\"label\":\"%s\",\"domain\":\"%s\",\"code\":%lu,\"span\":%ld,"
		"\"utc\":\"%04u-%02u-%02uT%02u:%02u:%02u.%03uZ\",\"tick_ms\":%llu,\"elapsed_ms\":%llu,\"pid\":%lu,\"tid\":%lu}",
		event,label,domain,(unsigned long)code,(long)id,
		utc.wYear,utc.wMonth,utc.wDay,utc.wHour,utc.wMinute,utc.wSecond,utc.wMilliseconds,
		(unsigned long long)GetTickCount64(),(unsigned long long)elapsed,
		(unsigned long)GetCurrentProcessId(),(unsigned long)GetCurrentThreadId());
	if(n>0 && n<(int)sizeof(line)) {
		if(session) {
			WCHAR text[768]; MSIHANDLE record=MsiCreateRecord(1);
			if(record && MultiByteToWideChar(CP_UTF8,0,line,-1,text,ARRAYSIZE(text)) &&
				MsiRecordSetStringW(record,0,L"[1]")==ERROR_SUCCESS &&
				MsiRecordSetStringW(record,1,text)==ERROR_SUCCESS) MsiProcessMessage(session,INSTALLMESSAGE_INFO,record);
			if(record) MsiCloseHandle(record);
		} else if(setup_diagnostics_enabled()) {
			HANDLE output=GetStdHandle(STD_ERROR_HANDLE);
			if(output && output!=INVALID_HANDLE_VALUE && GetFileType(output)==FILE_TYPE_DISK) {
				DWORD written=0; line[n++]='\n'; WriteFile(output,line,(DWORD)n,&written,NULL);
			}
		}
	}
	SetLastError(error);
}
VX_DIAG_SPAN setup_diag_start(const char *label)
{
	DWORD error=GetLastError();
	VX_DIAG_SPAN span={GetTickCount64(),InterlockedIncrement(&diagnostic_sequence),label};
	diagnostic(0,"START",label,"none",0,span.id,0);
	SetLastError(error); return span;
}
void setup_diag_end(VX_DIAG_SPAN span,const char *domain,DWORD code)
{
	diagnostic(0,"END",span.label,domain,code,span.id,GetTickCount64()-span.start);
}
VX_DIAG_SPAN setup_diag_session_start(MSIHANDLE session,const char *label)
{
	DWORD error=GetLastError();
	VX_DIAG_SPAN span={GetTickCount64(),InterlockedIncrement(&diagnostic_sequence),label};
	diagnostic(session,"START",label,"none",0,span.id,0);
	SetLastError(error); return span;
}
void setup_diag_session_end(MSIHANDLE session,VX_DIAG_SPAN span,const char *domain,DWORD code)
{
	diagnostic(session,"END",span.label,domain,code,span.id,GetTickCount64()-span.start);
}
void setup_diag_event(MSIHANDLE session,const char *label,const char *domain,DWORD code)
{
	diagnostic(session,"RESULT",label,domain,code,0,0);
}
void setup_diag_handle(const char *role,const char *transition,HANDLE handle,DWORD access,DWORD share,DWORD creation)
{
	DWORD error=GetLastError();
	if(setup_diagnostics_enabled()) {
		BY_HANDLE_FILE_INFORMATION identity={0};
		BOOL valid=handle && handle!=INVALID_HANDLE_VALUE;
		BOOL identified=valid && GetFileInformationByHandle(handle,&identity);
		DWORD query=identified?ERROR_SUCCESS:valid?GetLastError():ERROR_INVALID_HANDLE;
		SYSTEMTIME utc; GetSystemTime(&utc);
		char line[768];
		int n=snprintf(line,sizeof(line),
			"VX_SETUP_HANDLE {\"schema\":1,\"event\":\"HANDLE\",\"span\":0,\"label\":\"%s:%s\","
			"\"role\":\"%s\",\"transition\":\"%s\",\"pid\":%lu,\"tid\":%lu,\"tick_ms\":%llu,"
			"\"utc\":\"%04u-%02u-%02uT%02u:%02u:%02u.%03uZ\","
			"\"handle_diagnostic_only\":\"%p\",\"access\":%lu,\"share\":%lu,\"creation\":%lu,\"valid\":%u,"
			"\"identity_win32_code\":%lu,\"volume\":%lu,\"file_id_high\":%lu,\"file_id_low\":%lu}\n",
			role,transition,role,transition,(unsigned long)GetCurrentProcessId(),(unsigned long)GetCurrentThreadId(),
			(unsigned long long)GetTickCount64(),utc.wYear,utc.wMonth,utc.wDay,utc.wHour,utc.wMinute,utc.wSecond,utc.wMilliseconds,
			(void *)handle,(unsigned long)access,(unsigned long)share,(unsigned long)creation,
			valid?1U:0U,(unsigned long)query,(unsigned long)identity.dwVolumeSerialNumber,
			(unsigned long)identity.nFileIndexHigh,(unsigned long)identity.nFileIndexLow);
		HANDLE output=GetStdHandle(STD_ERROR_HANDLE); DWORD written=0;
		if(n>0 && n<(int)sizeof(line) && output && output!=INVALID_HANDLE_VALUE && GetFileType(output)==FILE_TYPE_DISK)
			WriteFile(output,line,(DWORD)n,&written,NULL);
	}
	SetLastError(error);
}
void setup_diag_token(MSIHANDLE session,HANDLE token)
{
	DWORD error=GetLastError(),bytes=0,value=0;
	BOOL ok=GetTokenInformation(token,TokenType,&value,sizeof(value),&bytes);
	setup_diag_event(session,"native_effective_token_type",ok?"TOKEN_TYPE":"Win32",ok?value:GetLastError());
	ok=GetTokenInformation(token,TokenImpersonationLevel,&value,sizeof(value),&bytes);
	setup_diag_event(session,"native_effective_impersonation_level",ok?"SECURITY_IMPERSONATION_LEVEL":"Win32",ok?value:GetLastError());
	union { DWORD alignment; unsigned char data[8192]; } buffer;
	ok=GetTokenInformation(token,TokenPrivileges,buffer.data,sizeof(buffer.data),&bytes);
	setup_diag_event(session,"native_effective_privilege_query","Win32",ok?ERROR_SUCCESS:GetLastError());
	if(ok && bytes>=offsetof(TOKEN_PRIVILEGES,Privileges) && bytes<=sizeof(buffer.data)) {
		const WCHAR *names[]={SE_DEBUG_NAME,SE_RESTORE_NAME,SE_BACKUP_NAME,SE_IMPERSONATE_NAME};
		const char *labels[]={"native_debug_privilege_attributes","native_restore_privilege_attributes",
			"native_backup_privilege_attributes","native_impersonate_privilege_attributes"};
		TOKEN_PRIVILEGES *privileges=(TOKEN_PRIVILEGES *)buffer.data;
		if(privileges->PrivilegeCount<=(bytes-offsetof(TOKEN_PRIVILEGES,Privileges))/sizeof(LUID_AND_ATTRIBUTES)) {
			for(unsigned int j=0;j<ARRAYSIZE(names);++j) {
				LUID luid; BOOL found=FALSE;
				if(LookupPrivilegeValueW(NULL,names[j],&luid)) {
					for(DWORD i=0;i<privileges->PrivilegeCount;++i)
						if(privileges->Privileges[i].Luid.LowPart==luid.LowPart && privileges->Privileges[i].Luid.HighPart==luid.HighPart) {
							setup_diag_event(session,labels[j],"TOKEN_PRIVILEGE_ATTRIBUTES",privileges->Privileges[i].Attributes);
							found=TRUE; break;
						}
					if(!found) setup_diag_event(session,labels[j],"not_present",0);
				} else setup_diag_event(session,labels[j],"Win32",GetLastError());
			}
		}
	}
	SetLastError(error);
}

BOOL setup_property(MSIHANDLE h,const WCHAR *name,WCHAR *out,DWORD count)
{
	DWORD n=count;
	return MsiGetPropertyW(h,name,out,&n)==ERROR_SUCCESS && n<count && out[n]==0;
}
static BOOL field(MSIHANDLE db,const WCHAR *name,WCHAR *out,DWORD count)
{
	MSIHANDLE view=0,args=0,row=0,extra=0; BOOL ok=FALSE;
	if(MsiDatabaseOpenViewW(db,L"SELECT `Value` FROM `Property` WHERE `Property`=?",&view)!=ERROR_SUCCESS) goto done;
	args=MsiCreateRecord(1);
	if(!args || MsiRecordSetStringW(args,1,name)!=ERROR_SUCCESS || MsiViewExecute(view,args)!=ERROR_SUCCESS ||
		MsiViewFetch(view,&row)!=ERROR_SUCCESS) goto done;
	DWORD n=count;
	ok=MsiRecordGetStringW(row,1,out,&n)==ERROR_SUCCESS && n<count && MsiViewFetch(view,&extra)==ERROR_NO_MORE_ITEMS;
done:
	if(extra && MsiCloseHandle(extra)!=ERROR_SUCCESS) ok=FALSE;
	if(row && MsiCloseHandle(row)!=ERROR_SUCCESS) ok=FALSE;
	if(args && MsiCloseHandle(args)!=ERROR_SUCCESS) ok=FALSE;
	if(view && MsiCloseHandle(view)!=ERROR_SUCCESS) ok=FALSE;
	return ok;
}
static BOOL root_cleanup_row(MSIHANDLE db)
{
	MSIHANDLE view=0,row=0,extra=0; BOOL ok=FALSE;
	if(MsiDatabaseOpenViewW(db,L"SELECT `FileKey`,`Component_`,`FileName`,`DirProperty`,`InstallMode` FROM `RemoveFile`",&view)!=ERROR_SUCCESS ||
		MsiViewExecute(view,0)!=ERROR_SUCCESS || MsiViewFetch(view,&row)!=ERROR_SUCCESS) goto done;
	const WCHAR *values[]={L"VxRemoveOwnedRoot",L"Maintenance",NULL,L"INSTALLDIR"};
	ok=MsiRecordIsNull(row,3) && MsiRecordGetInteger(row,5)==2;
	for(unsigned int i=0;ok && i<ARRAYSIZE(values);++i) if(values[i]) {
		WCHAR value[64]; DWORD n=ARRAYSIZE(value);
		ok=MsiRecordGetStringW(row,i+1,value,&n)==ERROR_SUCCESS && n<ARRAYSIZE(value) && !wcscmp(value,values[i]);
	}
	ok=ok && MsiViewFetch(view,&extra)==ERROR_NO_MORE_ITEMS;
done:
	if(extra && MsiCloseHandle(extra)!=ERROR_SUCCESS) ok=FALSE;
	if(row && MsiCloseHandle(row)!=ERROR_SUCCESS) ok=FALSE;
	if(view && MsiCloseHandle(view)!=ERROR_SUCCESS) ok=FALSE;
	return ok;
}
BOOL setup_receipt(MSIHANDLE db,VX_RECEIPT *r,WCHAR owner_hash[65])
{
	WCHAR text[128]; MSIHANDLE summary=0; UINT type=0; int integer=0; FILETIME time={0};
	memset(r,0,sizeof(*r)); memcpy(r->magic,"VXMSI01",8); r->size=sizeof(*r); r->schema=1;
	const WCHAR *properties[]={L"ProductCode",L"ProductVersion",L"VX_SOURCE_REVISION"};
	char *dest[]={r->product,r->version,r->revision}; unsigned int widths[]={39,32,41};
	for(unsigned int i=0;i<ARRAYSIZE(properties);++i)
		if(!field(db,properties[i],text,ARRAYSIZE(text)) || !vx_ascii(dest[i],widths[i],text)) return FALSE;
	const WCHAR *hashes[]={L"VX_HASH_GUI",L"VX_HASH_CONTROL",L"VX_HASH_LICENSE",L"VX_HASH_NOTICES",L"VX_HASH_MANIFEST",L"VX_HASH_SUMS"};
	for(unsigned int i=0;i<VX_COUNT;++i)
		if(!field(db,hashes[i],text,ARRAYSIZE(text)) || !vx_ascii(r->hashes[i],65,text)) return FALSE;
	if(!field(db,L"UpgradeCode",text,ARRAYSIZE(text)) || wcscmp(text,VX_UPGRADE) ||
		!field(db,L"VX_SETUP_PROTOCOL",text,ARRAYSIZE(text)) || wcscmp(text,VX_SETUP_PROTOCOL) ||
		!field(db,L"VX_OWNER_SHA256",owner_hash,65) ||
		!field(db,L"MSIRESTARTMANAGERCONTROL",text,ARRAYSIZE(text)) || wcscmp(text,L"Disable") ||
		!field(db,L"REBOOT",text,ARRAYSIZE(text)) || wcscmp(text,L"ReallySuppress") ||
		MsiGetSummaryInformationW(db,NULL,0,&summary)!=ERROR_SUCCESS) return FALSE;
	DWORD n=ARRAYSIZE(text);
	BOOL ok=MsiSummaryInfoGetPropertyW(summary,PIDSI_REVNUMBER,&type,&integer,&time,text,&n)==ERROR_SUCCESS &&
		type==VT_LPSTR && vx_ascii(r->package,39,text);
	n=ARRAYSIZE(text);
	ok=ok && MsiSummaryInfoGetPropertyW(summary,PIDSI_TEMPLATE,&type,&integer,&time,text,&n)==ERROR_SUCCESS &&
		type==VT_LPSTR && !wcscmp(text,L"x64;1033");
	n=ARRAYSIZE(text);
	ok=ok && MsiSummaryInfoGetPropertyW(summary,PIDSI_WORDCOUNT,&type,&integer,&time,text,&n)==ERROR_SUCCESS &&
		type==VT_I4 && integer==10;
	if(MsiCloseHandle(summary)!=ERROR_SUCCESS) ok=FALSE;
	char hash[65];
	return ok && vx_receipt(r) && vx_ascii(hash,65,owner_hash) && vx_hex(hash,64) && root_cleanup_row(db);
}
BOOL setup_catalog_owned(VX_CONTEXT *c,const VX_RECEIPT *before,const VX_RECEIPT *after)
{
	WCHAR product[39],value[MAX_PATH],a[39],b[39]; DWORD n=ARRAYSIZE(value);
	BOOL same_product=!strcmp(before->product,after->product);
	if(strcmp(before->revision,after->revision) ||
		!vx_wide(product,39,after->product) || !vx_wide(a,39,before->package) || !vx_wide(b,39,after->package) ||
		MsiGetProductInfoExW(product,c->sid,MSIINSTALLCONTEXT_USERUNMANAGED,INSTALLPROPERTY_PRODUCTSTATE,value,&n)!=ERROR_SUCCESS ||
		(wcscmp(value,L"1") && wcscmp(value,L"5"))) return FALSE;
	const WCHAR *names[]={INSTALLPROPERTY_ASSIGNMENTTYPE,INSTALLPROPERTY_PACKAGECODE,INSTALLPROPERTY_TRANSFORMS};
	for(unsigned int i=0;i<ARRAYSIZE(names);++i) {
		n=ARRAYSIZE(value);
		if(MsiGetProductInfoExW(product,c->sid,MSIINSTALLCONTEXT_USERUNMANAGED,names[i],value,&n)!=ERROR_SUCCESS ||
			(i==0 && wcscmp(value,L"0")) || (i==1 && wcscmp(value,b) && (!same_product || wcscmp(value,a))) ||
			(i==2 && value[0])) return FALSE;
	}
	return TRUE;
}
BOOL setup_single_product(const VX_RECEIPT *r)
{
	WCHAR expected[39],product[39],sid[256],name[64]; DWORD count=0;
	if(!vx_wide(expected,39,r->product)) return FALSE;
	for(DWORD i=0;i<128;++i) {
		MSIINSTALLCONTEXT actual=MSIINSTALLCONTEXT_NONE; DWORD n=ARRAYSIZE(sid);
		UINT code=MsiEnumProductsExW(NULL,NULL,MSIINSTALLCONTEXT_USERUNMANAGED,i,product,&actual,sid,&n);
		if(code==ERROR_NO_MORE_ITEMS) return count==1;
		if(code!=ERROR_SUCCESS || actual!=MSIINSTALLCONTEXT_USERUNMANAGED) return FALSE;
		n=ARRAYSIZE(name);
		if(MsiGetProductInfoExW(product,NULL,actual,L"ProductName",name,&n)!=ERROR_SUCCESS) return FALSE;
		if(!wcscmp(product,expected) || !wcscmp(name,L"VX Bridge")) {
			if(wcscmp(product,expected) || ++count!=1) return FALSE;
		}
	}
	return FALSE;
}
BOOL setup_related_owned(VX_CONTEXT *c,const VX_RECEIPT *before,const VX_RECEIPT *after)
{
	WCHAR product[39],value[32],a[39]={0},b[39]; DWORD n;
	if(!vx_wide(b,39,after->product) || (before && !vx_wide(a,39,before->product))) return FALSE;
	for(DWORD i=0;i<128;++i) {
		UINT code=MsiEnumRelatedProductsW(VX_UPGRADE,0,i,product);
		if(code==ERROR_NO_MORE_ITEMS) return TRUE;
		if(code!=ERROR_SUCCESS) return FALSE;
		const VX_RECEIPT *r=!wcscmp(product,b)?after:before && !wcscmp(product,a)?before:NULL;
		if(!r || !setup_catalog_owned(c,r,r)) return FALSE;
		n=ARRAYSIZE(value);
		if(MsiGetProductInfoExW(product,NULL,MSIINSTALLCONTEXT_MACHINE,INSTALLPROPERTY_PRODUCTSTATE,value,&n)!=ERROR_UNKNOWN_PRODUCT)
			return FALSE;
		n=ARRAYSIZE(value);
		if(MsiGetProductInfoExW(product,c->sid,MSIINSTALLCONTEXT_USERMANAGED,INSTALLPROPERTY_PRODUCTSTATE,value,&n)!=ERROR_UNKNOWN_PRODUCT)
			return FALSE;
	}
	return FALSE;
}
HANDLE setup_lifetime(VX_CONTEXT *c)
{
	WCHAR path[MAX_PATH];
	if(!vx_path(path,c->state,L"host.lock")) return INVALID_HANDLE_VALUE;
	HANDLE handle=vx_file(path,FALSE,GENERIC_READ|GENERIC_WRITE,0,OPEN_ALWAYS);
	setup_diag_handle("host_lease","open",handle,GENERIC_READ|GENERIC_WRITE,0,OPEN_ALWAYS);
	return handle;
}
static BOOL pin_readonly_chain(VX_CONTEXT *c,const WCHAR *path)
{
	unsigned int first=c->count;
	if(!vx_pin_tree(c,path)) return FALSE;
	for(unsigned int i=first;i<c->count;++i) {
		WCHAR final[MAX_PATH+8];
		DWORD n=GetFinalPathNameByHandleW(c->pins[i],final,ARRAYSIZE(final),0);
		if(!n || n>=ARRAYSIZE(final) || wcsncmp(final,L"\\\\?\\",4)) return FALSE;
		/* Deny directory write handles as well as deletion: otherwise an empty
		 * pinned directory could be turned into a reparse point in place.
		 */
		HANDLE strict=vx_file(final+4,TRUE,FILE_READ_ATTRIBUTES,FILE_SHARE_READ,OPEN_EXISTING);
		if(strict==INVALID_HANDLE_VALUE) return FALSE;
		if(!CloseHandle(c->pins[i])) { CloseHandle(strict); return FALSE; }
		c->pins[i]=strict;
	}
	return TRUE;
}
BOOL setup_pin_directory(VX_CONTEXT *c,const WCHAR *path) { return pin_readonly_chain(c,path); }
BOOL setup_pin_root(VX_CONTEXT *c,BOOL *created,unsigned int *slot)
{
	WCHAR parent[MAX_PATH],expected[MAX_PATH];
	*created=FALSE; *slot=(unsigned int)-1;
	if(!vx_path(parent,c->base,L"Programs") || !vx_path(expected,parent,L"VxInfiniteTeesBridge") ||
		wcscmp(expected,c->root)) return FALSE;
	/* Pin the existing chain before creating its next child; never create
	 * through a pre-existing Programs/root junction, even if it is empty.
	 */
	if(!pin_readonly_chain(c,c->base) ||
		(!CreateDirectoryW(parent,NULL) && GetLastError()!=ERROR_ALREADY_EXISTS) ||
		!pin_readonly_chain(c,parent)) return FALSE;
	if(CreateDirectoryW(c->root,NULL)) *created=TRUE;
	else if(GetLastError()!=ERROR_ALREADY_EXISTS) return FALSE;
	if(!pin_readonly_chain(c,c->root)) return FALSE;
	*slot=c->count-1;
	return TRUE;
}
BOOL setup_root_pinned(VX_CONTEXT *c)
{
	for(unsigned int i=0;i<c->count;++i) {
		WCHAR final[MAX_PATH+8]; BY_HANDLE_FILE_INFORMATION info;
		DWORD n=GetFinalPathNameByHandleW(c->pins[i],final,ARRAYSIZE(final),0);
		if(n && n<ARRAYSIZE(final) && !wcsncmp(final,L"\\\\?\\",4) && !_wcsicmp(final+4,c->root)) {
			return GetFileInformationByHandle(c->pins[i],&info) &&
				(info.dwFileAttributes&FILE_ATTRIBUTE_DIRECTORY) &&
				!(info.dwFileAttributes&FILE_ATTRIBUTE_REPARSE_POINT);
		}
	}
	return FALSE;
}
BOOL setup_remove_root(VX_CONTEXT *c,unsigned int *slot)
{
	if(*slot>=c->count || !setup_root_pinned(c)) return FALSE;
	BY_HANDLE_FILE_INFORMATION before,after;
	if(!GetFileInformationByHandle(c->pins[*slot],&before) || !CloseHandle(c->pins[*slot])) return FALSE;
	c->pins[*slot]=c->pins[--c->count]; *slot=(unsigned int)-1;
	/* MSI has returned a terminal result before this handoff. Reopen with
	 * DELETE only now and compare the exact object before handle-based delete.
	 * A replacement path/junction is not permission to remove another object.
	 */
	HANDLE directory=vx_file(c->root,TRUE,FILE_READ_ATTRIBUTES|DELETE,FILE_SHARE_READ,OPEN_EXISTING);
	if(directory==INVALID_HANDLE_VALUE) return FALSE;
	BOOL ok=GetFileInformationByHandle(directory,&after) &&
		before.dwVolumeSerialNumber==after.dwVolumeSerialNumber &&
		before.nFileIndexHigh==after.nFileIndexHigh && before.nFileIndexLow==after.nFileIndexLow;
	FILE_DISPOSITION_INFO disposition={TRUE};
	ok=ok && SetFileInformationByHandle(directory,FileDispositionInfo,&disposition,sizeof(disposition));
	if(!CloseHandle(directory)) ok=FALSE;
	if(ok) {
		DWORD attributes=GetFileAttributesW(c->root),error=GetLastError();
		ok=attributes==INVALID_FILE_ATTRIBUTES && (error==ERROR_FILE_NOT_FOUND || error==ERROR_PATH_NOT_FOUND);
	}
	return ok;
}
static BOOL same_directory(const BY_HANDLE_FILE_INFORMATION *a,const BY_HANDLE_FILE_INFORMATION *b)
{
	return a->dwVolumeSerialNumber==b->dwVolumeSerialNumber &&
		a->nFileIndexHigh==b->nFileIndexHigh && a->nFileIndexLow==b->nFileIndexLow;
}
BOOL setup_root_snapshot(VX_CONTEXT *c,unsigned int slot,BY_HANDLE_FILE_INFORMATION *identity)
{
	WCHAR final[MAX_PATH+8];
	if(slot>=c->count) return FALSE;
	DWORD n=GetFinalPathNameByHandleW(c->pins[slot],final,ARRAYSIZE(final),0);
	return n && n<ARRAYSIZE(final) && !wcsncmp(final,L"\\\\?\\",4) && !_wcsicmp(final+4,c->root) &&
		GetFileInformationByHandle(c->pins[slot],identity) &&
		(identity->dwFileAttributes&FILE_ATTRIBUTE_DIRECTORY) &&
		!(identity->dwFileAttributes&FILE_ATTRIBUTE_REPARSE_POINT);
}
BOOL setup_release_root(VX_CONTEXT *c,unsigned int *slot,const BY_HANDLE_FILE_INFORMATION *identity)
{
	BY_HANDLE_FILE_INFORMATION actual;
	if(!setup_root_snapshot(c,*slot,&actual) || !same_directory(&actual,identity)) return FALSE;
	/* Only the verified root handle is handed to conventional MSI namespace
	 * work. Ancestor/source/lease handles remain owned and pinned.
	 */
	setup_diag_handle("owned_root","before-close",c->pins[*slot],FILE_READ_ATTRIBUTES,FILE_SHARE_READ,OPEN_EXISTING);
	if(!CloseHandle(c->pins[*slot])) { setup_diag_event(0,"owned_root_close","Win32",GetLastError()); return FALSE; }
	setup_diag_event(0,"owned_root_close","Win32",ERROR_SUCCESS);
	c->pins[*slot]=c->pins[--c->count]; *slot=(unsigned int)-1;
	return TRUE;
}
BOOL setup_repin_root(VX_CONTEXT *c,unsigned int *slot,const BY_HANDLE_FILE_INFORMATION *identity)
{
	if(*slot!=(unsigned int)-1 || c->count>=ARRAYSIZE(c->pins)) return FALSE;
	HANDLE handle=vx_file(c->root,TRUE,FILE_READ_ATTRIBUTES,FILE_SHARE_READ,OPEN_EXISTING);
	setup_diag_handle("owned_root","repin-open",handle,FILE_READ_ATTRIBUTES,FILE_SHARE_READ,OPEN_EXISTING);
	if(handle==INVALID_HANDLE_VALUE) return FALSE;
	BY_HANDLE_FILE_INFORMATION actual;
	BOOL ok=GetFileInformationByHandle(handle,&actual) && (!identity || same_directory(&actual,identity));
	if(!ok) { CloseHandle(handle); return FALSE; }
	*slot=c->count; c->pins[c->count++]=handle;
	return TRUE;
}
BOOL setup_recreate_root(VX_CONTEXT *c,unsigned int *slot,BY_HANDLE_FILE_INFORMATION *identity)
{
	/* Exclusive forward recreation only, never rollback repair-by-creation.
	 * CreateDirectory does not return a handle: validate the acquired canonical
	 * non-reparse empty object, without claiming atomic create-and-pin identity.
	 */
	if(*slot!=(unsigned int)-1 || !CreateDirectoryW(c->root,NULL) ||
		!setup_repin_root(c,slot,NULL) || !setup_root_snapshot(c,*slot,identity)) return FALSE;
	WCHAR search[MAX_PATH]; WIN32_FIND_DATAW entry;
	if(!vx_path(search,c->root,L"*")) return FALSE;
	HANDLE find=FindFirstFileW(search,&entry);
	if(find==INVALID_HANDLE_VALUE) return GetLastError()==ERROR_FILE_NOT_FOUND;
	BOOL empty=TRUE;
	do {
		if(wcscmp(entry.cFileName,L".") && wcscmp(entry.cFileName,L"..")) { empty=FALSE; break; }
	} while(FindNextFileW(find,&entry));
	DWORD error=GetLastError();
	if(!FindClose(find)) empty=FALSE;
	return empty && error==ERROR_NO_MORE_FILES;
}
BOOL setup_payload(VX_CONTEXT *c,const VX_RECEIPT *r,BOOL allow_missing)
{
	/* Repair may replace recorded missing owned files; never adopt another name. */
	WCHAR path[MAX_PATH]; WIN32_FIND_DATAW entry;
	if(!setup_root_pinned(c)) return FALSE;
	if(!vx_path(path,c->root,L"*")) return FALSE;
	HANDLE find=FindFirstFileW(path,&entry);
	if(find==INVALID_HANDLE_VALUE) return allow_missing && (GetLastError()==ERROR_FILE_NOT_FOUND || GetLastError()==ERROR_PATH_NOT_FOUND);
	BOOL ok=TRUE;
	do {
		if(!wcscmp(entry.cFileName,L".") || !wcscmp(entry.cFileName,L"..")) continue;
		unsigned int i; for(i=0;i<VX_COUNT;++i) if(!wcscmp(entry.cFileName,vx_files[i])) break;
		if(i==VX_COUNT || entry.dwFileAttributes&(FILE_ATTRIBUTE_DIRECTORY|FILE_ATTRIBUTE_REPARSE_POINT)) { ok=FALSE; break; }
	} while(FindNextFileW(find,&entry));
	if(ok && GetLastError()!=ERROR_NO_MORE_FILES) ok=FALSE;
	if(!FindClose(find)) ok=FALSE;
	for(unsigned int i=0;ok && i<VX_COUNT;++i) {
		if(!vx_path(path,c->root,vx_files[i])) return FALSE;
		HANDLE file=vx_file(path,FALSE,GENERIC_READ,FILE_SHARE_READ,OPEN_EXISTING);
		if(file==INVALID_HANDLE_VALUE) {
			DWORD e=GetLastError();
			setup_diag_event(0,"payload_file_open","Win32",e);
			if(allow_missing && e==ERROR_FILE_NOT_FOUND) continue;
			return FALSE;
		}
		char hash[65]; ok=vx_hash(file,hash) && (allow_missing || !strcmp(hash,r->hashes[i]));
		if(!ok) setup_diag_event(0,i==0?"payload_gui_hash":i==1?"payload_control_hash":i==2?"payload_license_hash":
			i==3?"payload_notices_hash":i==4?"payload_manifest_hash":"payload_sums_hash","check",1);
		if(!CloseHandle(file)) ok=FALSE;
	}
	return ok;
}
BOOL setup_user(HANDLE process,const WCHAR *sid)
{
	HANDLE token=NULL; DWORD n=0; TOKEN_USER *u=NULL; WCHAR *actual=NULL; BOOL ok=FALSE;
	if(!OpenProcessToken(process,TOKEN_QUERY,&token)) goto done;
	if(GetTokenInformation(token,TokenUser,NULL,0,&n) || GetLastError()!=ERROR_INSUFFICIENT_BUFFER || n>65536) goto done;
	u=HeapAlloc(GetProcessHeap(),HEAP_ZERO_MEMORY,n);
	if(!u || !GetTokenInformation(token,TokenUser,u,n,&n) || !ConvertSidToStringSidW(u->User.Sid,&actual)) goto done;
	ok=!wcscmp(actual,sid);
done:
	if(actual) LocalFree(actual);
	if(u) HeapFree(GetProcessHeap(),0,u);
	if(token && !CloseHandle(token)) ok=FALSE;
	return ok;
}
BOOL setup_transfer(HANDLE pipe,BOOL write,void *data,DWORD size)
{
	DWORD at=0; ULONGLONG start=GetTickCount64();
	while(at<size && GetTickCount64()-start<3000) {
		DWORD n=0;
		BOOL ok=write?WriteFile(pipe,(BYTE *)data+at,size-at,&n,NULL):ReadFile(pipe,(BYTE *)data+at,size-at,&n,NULL);
		if(!ok && GetLastError()!=ERROR_NO_DATA && GetLastError()!=ERROR_PIPE_LISTENING) return FALSE;
		if(ok && n) at+=n; else Sleep(10);
	}
	return at==size;
}
static BOOL owned_command(VX_CONTEXT *c,const WCHAR *value)
{
	WCHAR image[MAX_PATH],prefix[MAX_PATH+40];
	if(!vx_path(image,c->root,L"GSPconnect.exe") ||
		swprintf(prefix,ARRAYSIZE(prefix),L"\"%ls\" --desktop --target-port ",image)<=0) return FALSE;
	size_t n=wcslen(prefix); if(wcsncmp(value,prefix,n)) return FALSE;
	unsigned int port=0,digits=0;
	for(const WCHAR *p=value+n;*p;++p) {
		if(*p<L'0' || *p>L'9' || (digits && !port) || ++digits>5) return FALSE;
		port=port*10+(unsigned int)(*p-L'0');
	}
	return digits && port && port<=65535 && port!=12321;
}
BOOL setup_startup(VX_CONTEXT *c,VX_PENDING *p,int choice,BOOL restore,BOOL inspect)
{
	WCHAR name[320],value[261]={0}; HKEY user=NULL,key=NULL; HANDLE lock=NULL; BOOL ok=FALSE;
	if(swprintf(name,ARRAYSIZE(name),L"Global\\VxBridge.Startup.%ls",c->sid)<=0) return FALSE;
	lock=CreateMutexW(NULL,FALSE,name); if(!lock) return FALSE;
	DWORD wait=WaitForSingleObject(lock,500);
	if(wait!=WAIT_OBJECT_0 && wait!=WAIT_ABANDONED) { CloseHandle(lock); return FALSE; }
	if(RegOpenCurrentUser(KEY_READ|KEY_WRITE,&user)!=ERROR_SUCCESS) goto done;
	LSTATUS status=RegOpenKeyExW(user,L"Software\\Microsoft\\Windows\\CurrentVersion\\Run",0,KEY_QUERY_VALUE|KEY_SET_VALUE,&key);
	if(status==ERROR_FILE_NOT_FOUND && !inspect && !restore && choice==1)
		status=RegCreateKeyExW(user,L"Software\\Microsoft\\Windows\\CurrentVersion\\Run",0,NULL,0,KEY_QUERY_VALUE|KEY_SET_VALUE,NULL,&key,NULL);
	if(status==ERROR_FILE_NOT_FOUND) { ok=!(restore && p->run_saved); goto done; }
	if(status!=ERROR_SUCCESS) goto done;
	DWORD kind=0,n=sizeof(value);
	status=RegQueryValueExW(key,L"VxInfiniteTeesBridge",NULL,&kind,(BYTE *)value,&n);
	if(status!=ERROR_FILE_NOT_FOUND && (status!=ERROR_SUCCESS || kind!=REG_SZ || n<2 || n%2 || n>sizeof(value) ||
		value[n/2-1] || (wcslen(value)+1)*sizeof(WCHAR)!=n || !owned_command(c,value))) goto done;
	if(inspect) {
		p->run_saved=status==ERROR_SUCCESS;
		if(p->run_saved) wcscpy(p->run_command,value);
		ok=TRUE; goto done;
	}
	if(restore) {
		WCHAR image[MAX_PATH],created[261];
		if(!vx_path(image,c->root,L"GSPconnect.exe") ||
			swprintf(created,ARRAYSIZE(created),L"\"%ls\" --desktop --target-port 999",image)<=0) goto done;
		if(status==ERROR_SUCCESS && wcscmp(value,p->run_saved?p->run_command:created)) goto done;
		if(p->run_saved) {
			if(!owned_command(c,p->run_command)) goto done;
			ok=RegSetValueExW(key,L"VxInfiniteTeesBridge",0,REG_SZ,(BYTE *)p->run_command,
				(DWORD)((wcslen(p->run_command)+1)*2))==ERROR_SUCCESS;
		} else ok=status==ERROR_FILE_NOT_FOUND || RegDeleteValueW(key,L"VxInfiniteTeesBridge")==ERROR_SUCCESS;
	} else if(choice==1) {
		WCHAR image[MAX_PATH],command[261];
		if(!vx_path(image,c->root,L"GSPconnect.exe") ||
			swprintf(command,ARRAYSIZE(command),L"\"%ls\" --desktop --target-port 999",image)<=0) goto done;
		const WCHAR *want=p->run_saved?p->run_command:command;
		if((p->run_saved && status==ERROR_FILE_NOT_FOUND) ||
			(status==ERROR_SUCCESS && wcscmp(value,want))) goto done;
		ok=RegSetValueExW(key,L"VxInfiniteTeesBridge",0,REG_SZ,(BYTE *)want,(DWORD)((wcslen(want)+1)*2))==ERROR_SUCCESS;
	} else {
		if(status==ERROR_SUCCESS && (!p->run_saved || wcscmp(value,p->run_command))) goto done;
		ok=status==ERROR_FILE_NOT_FOUND || RegDeleteValueW(key,L"VxInfiniteTeesBridge")==ERROR_SUCCESS;
	}
	ok=ok && RegFlushKey(key)==ERROR_SUCCESS;
done:
	if(key && RegCloseKey(key)!=ERROR_SUCCESS) ok=FALSE;
	if(user && RegCloseKey(user)!=ERROR_SUCCESS) ok=FALSE;
	if(!ReleaseMutex(lock)) ok=FALSE;
	if(!CloseHandle(lock)) ok=FALSE;
	return ok;
}
BOOL setup_startup_state(VX_CONTEXT *c,BOOL *enabled,WCHAR command[261])
{
	VX_PENDING p={0};
	if(!setup_startup(c,&p,0,FALSE,TRUE)) return FALSE;
	*enabled=p.run_saved;
	wcscpy(command,p.run_command);
	return TRUE;
}
BOOL setup_cache_record(VX_CONTEXT *c,const WCHAR *name,const WCHAR *path)
{
	WCHAR value[96]; char hash[65],previous[65]; BOOL exists=FALSE;
	if(swprintf(value,ARRAYSIZE(value),L"Cache.%ls",name)<=0) return FALSE;
	HANDLE file=vx_file(path,FALSE,GENERIC_READ,FILE_SHARE_READ,OPEN_EXISTING);
	if(file==INVALID_HANDLE_VALUE) return FALSE;
	BOOL ok=vx_hash(file,hash);
	if(!CloseHandle(file)) ok=FALSE;
	return ok && vx_binary(c,value,previous,sizeof(previous),&exists) &&
		(!exists || !memcmp(previous,hash,sizeof(hash))) && vx_put(c,value,hash,sizeof(hash));
}
BOOL setup_owned_quit(VX_CONTEXT *c,const VX_RECEIPT *before,VX_QUIT_WAIT wait,void *data)
{
	WCHAR name[320],image[MAX_PATH],actual[MAX_PATH]; DWORD pid=0,n=ARRAYSIZE(actual),again=0,exit=1;
	HANDLE pipe=INVALID_HANDLE_VALUE,process=NULL,file=INVALID_HANDLE_VALUE; BOOL ok=FALSE;
	if(swprintf(name,ARRAYSIZE(name),L"\\\\.\\pipe\\VxInfiniteTeesBridge.P01.%ls",c->sid)<=0 ||
		!vx_path(image,c->root,L"GSPconnect.exe")) goto done;
	pipe=CreateFileW(name,GENERIC_READ|GENERIC_WRITE,0,NULL,OPEN_EXISTING,0,NULL);
	DWORD mode=PIPE_READMODE_BYTE|PIPE_NOWAIT;
	if(pipe==INVALID_HANDLE_VALUE || !SetNamedPipeHandleState(pipe,&mode,NULL,NULL) ||
		!GetNamedPipeServerProcessId(pipe,&pid)) goto done;
	process=OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION|SYNCHRONIZE,FALSE,pid);
	if(!process || !setup_user(process,c->sid) || !QueryFullProcessImageNameW(process,0,actual,&n) || _wcsicmp(actual,image)) goto done;
	file=vx_file(image,FALSE,GENERIC_READ,FILE_SHARE_READ,OPEN_EXISTING);
	char hash[65];
	if(file==INVALID_HANDLE_VALUE || !vx_hash(file,hash) || strcmp(hash,before->hashes[0]) ||
		!GetNamedPipeServerProcessId(pipe,&again) || pid!=again || WaitForSingleObject(process,0)!=WAIT_TIMEOUT) goto done;
	char command[]="quit\n";
	if(!setup_transfer(pipe,TRUE,command,5)) goto done;
	char reply[65537]; DWORD at=0; ULONGLONG start=GetTickCount64();
	while(at<65536 && GetTickCount64()-start<3000) {
		DWORD got=0;
		if(ReadFile(pipe,reply+at,65536-at,&got,NULL) && got) { at+=got; if(reply[at-1]=='\n') break; }
		else if(GetLastError()!=ERROR_NO_DATA) goto done;
		Sleep(10);
	}
	reply[at]=0;
	if(!at || reply[at-1]!='\n' || !strstr(reply,"\"Quit\":\"pending_source_departure\"")) goto done;
	if(!wait(process,data) || !GetExitCodeProcess(process,&exit) || exit) goto done;
	ok=TRUE;
done:
	if(file!=INVALID_HANDLE_VALUE && !CloseHandle(file)) ok=FALSE;
	if(process && !CloseHandle(process)) ok=FALSE;
	if(pipe!=INVALID_HANDLE_VALUE && !CloseHandle(pipe)) ok=FALSE;
	return ok;
}
