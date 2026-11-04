/* SPDX-License-Identifier: MIT */
#define COBJMACROS
#include "protocol.h"
#include <shlobj.h>

enum { REMOVE_ROOT, REMOVE_STATE, REMOVE_LOGS, REMOVE_CONFIG, REMOVE_LOCAL, REMOVE_SHORTCUTS, REMOVE_DIRS };
#define REMOVE_MAX_FILES 64
typedef struct { DWORD directory; WCHAR name[96]; } REMOVE_FILE;
typedef struct {
	DWORD size,count,quiet,had_receipt,had_pending;
	WCHAR sid[256];
	VX_RECEIPT receipt;
	VX_RECEIPT original;
	VX_PENDING startup;
	WCHAR owner[65];
	REMOVE_FILE files[REMOVE_MAX_FILES];
} REMOVE_PLAN;
_Static_assert(2*sizeof(REMOVE_PLAN)+1<32767,"bounded CustomActionData");

static BOOL missing(DWORD e) { return e==ERROR_FILE_NOT_FOUND || e==ERROR_PATH_NOT_FOUND; }
static HANDLE owner(VX_CONTEXT *c)
{
	WCHAR name[320];
	if(swprintf(name,ARRAYSIZE(name),L"Global\\VxBridge.SetupOwner.%ls",c->sid)<=0) return NULL;
	HANDLE handle=CreateMutexW(NULL,FALSE,name);
	if(!handle) return NULL;
	DWORD wait=WaitForSingleObject(handle,0);
	if(wait==WAIT_OBJECT_0 || wait==WAIT_ABANDONED) return handle;
	CloseHandle(handle); return NULL;
}
static BOOL close_owner(HANDLE handle)
{
	BOOL ok=ReleaseMutex(handle);
	if(!CloseHandle(handle)) ok=FALSE;
	return ok;
}
static UINT problem(MSIHANDLE h,const WCHAR *path)
{
	setup_diag_event(h,"remove_refusal","last_error_observation",GetLastError());
	WCHAR message[768];
	swprintf(message,ARRAYSIZE(message),
		L"VX Bridge uninstall incomplete. Ownership, access or removal could not be verified: %ls. "
		L"No forced shutdown or deferred/reboot cleanup is allowed. Disconnect in ProTee Labs and Quit normally; "
		L"use VX Bridge Setup / Repair if the installation is damaged.",path);
	return vx_error(h,message);
}
static BOOL directories(VX_CONTEXT *c,WCHAR paths[REMOVE_DIRS][MAX_PATH])
{
	WCHAR *roaming=NULL,*programs=NULL;
	wcscpy(paths[REMOVE_ROOT],c->root); wcscpy(paths[REMOVE_STATE],c->state);
	BOOL ok=vx_path(paths[REMOVE_LOCAL],c->base,L"VxInfiniteTeesBridge") &&
		vx_path(paths[REMOVE_LOGS],paths[REMOVE_LOCAL],L"logs") &&
		SHGetKnownFolderPath(&FOLDERID_RoamingAppData,KF_FLAG_DONT_VERIFY,c->user_token,&roaming)==S_OK &&
		vx_path(paths[REMOVE_CONFIG],roaming,L"VxInfiniteTeesBridge") &&
		SHGetKnownFolderPath(&FOLDERID_Programs,KF_FLAG_DONT_VERIFY,c->user_token,&programs)==S_OK &&
		wcslen(programs)<MAX_PATH;
	if(ok) wcscpy(paths[REMOVE_SHORTCUTS],programs);
	if(roaming) CoTaskMemFree(roaming);
	if(programs) CoTaskMemFree(programs);
	return ok;
}
static BOOL cache_name(const WCHAR *name)
{
	if(!wcscmp(name,L"setup.exe")) return TRUE;
	char guid[39]; WCHAR part[39];
	if(wcslen(name)!=42 || wcscmp(name+38,L".msi")) return FALSE;
	memcpy(part,name,38*sizeof(WCHAR)); part[38]=0;
	return vx_ascii(guid,39,part) && vx_guid(guid);
}
static BOOL destination_temp(const WCHAR *name)
{
	size_t n=wcslen(name);
	if(n<18 || n>27 || wcsncmp(name,L".destination-",13) || wcscmp(name+n-4,L".tmp")) return FALSE;
	ULONGLONG value=0;
	for(size_t i=13;i<n-4;++i) {
		if(name[i]<L'0' || name[i]>L'9') return FALSE;
		value=value*10+(unsigned int)(name[i]-L'0');
		if(value>UINT32_MAX) return FALSE;
	}
	return n==18 || name[13]!=L'0';
}
static BOOL known_file(DWORD directory,const WCHAR *name)
{
	if(directory==REMOVE_ROOT) {
		for(unsigned int i=0;i<VX_COUNT;++i) if(!wcscmp(name,vx_files[i])) return TRUE;
	} else if(directory==REMOVE_STATE) {
		return cache_name(name) || !wcscmp(name,L"host.lock") ||
			!wcscmp(name,L"setup-msi.log") || !wcscmp(name,L"setup-result.log");
	} else if(directory==REMOVE_LOGS) {
		return wcslen(name)==14 && !wcsncmp(name,L"bridge.",7) && name[7]>=L'0' &&
			name[7]<=L'4' && !wcscmp(name+8,L".jsonl");
	} else if(directory==REMOVE_CONFIG) return !wcscmp(name,L"destination.json") || destination_temp(name);
	else if(directory==REMOVE_SHORTCUTS) return !wcscmp(name,L"VX Bridge.lnk") || !wcscmp(name,L"VX Bridge Setup.lnk") ||
		!wcscmp(name,L"VX Bridge Diagnostics.lnk");
	return FALSE;
}
static BOOL add_file(REMOVE_PLAN *p,DWORD directory,const WCHAR *name)
{
	if(!known_file(directory,name) || wcslen(name)>=ARRAYSIZE(p->files[0].name)) return FALSE;
	for(DWORD i=0;i<p->count;++i)
		if(p->files[i].directory==directory && !wcscmp(p->files[i].name,name)) return TRUE;
	if(p->count==REMOVE_MAX_FILES) return FALSE;
	p->files[p->count].directory=directory;
	wcscpy(p->files[p->count++].name,name);
	return TRUE;
}
static BOOL contains(const REMOVE_PLAN *p,DWORD directory,const WCHAR *name)
{
	for(DWORD i=0;i<p->count;++i)
		if(p->files[i].directory==directory && !wcscmp(p->files[i].name,name)) return TRUE;
	return FALSE;
}
static BOOL fixed_files(REMOVE_PLAN *p)
{
	for(unsigned int i=0;i<VX_COUNT;++i) if(!add_file(p,REMOVE_ROOT,vx_files[i])) return FALSE;
	const WCHAR *state[]={L"host.lock",L"setup.exe",L"setup-msi.log",L"setup-result.log"};
	for(unsigned int i=0;i<ARRAYSIZE(state);++i) if(!add_file(p,REMOVE_STATE,state[i])) return FALSE;
	for(unsigned int i=0;i<5;++i) {
		WCHAR name[32];
		if(swprintf(name,32,L"bridge.%u.jsonl",i)<=0 || !add_file(p,REMOVE_LOGS,name)) return FALSE;
	}
	return add_file(p,REMOVE_CONFIG,L"destination.json") &&
		add_file(p,REMOVE_SHORTCUTS,L"VX Bridge.lnk") && add_file(p,REMOVE_SHORTCUTS,L"VX Bridge Setup.lnk") &&
		add_file(p,REMOVE_SHORTCUTS,L"VX Bridge Diagnostics.lnk");
}
/* Only callback-local handles. Missing descendants never authorize traversal
 * through an unvalidated existing ancestor or creation of a cleanup directory.
 */
static BOOL pin_existing(VX_CONTEXT *c,const WCHAR *path,BOOL *exists)
{
	WCHAR parent[MAX_PATH]; wcscpy(parent,path);
	*exists=TRUE;
	while(GetFileAttributesW(parent)==INVALID_FILE_ATTRIBUTES) {
		if(!missing(GetLastError())) return FALSE;
		*exists=FALSE;
		WCHAR *slash=wcsrchr(parent,L'\\');
		if(!slash || slash<=parent+2) return FALSE;
		*slash=0;
	}
	return setup_pin_directory(c,parent);
}
static BOOL unpin(VX_CONTEXT *c,unsigned int first)
{
	BOOL ok=TRUE;
	while(c->count>first) if(!CloseHandle(c->pins[--c->count])) ok=FALSE;
	return ok;
}
static BOOL open_state(VX_CONTEXT *c)
{
	HKEY user=NULL;
	if(RegOpenCurrentUser(KEY_READ|KEY_WRITE,&user)!=ERROR_SUCCESS) return FALSE;
	LSTATUS e=RegOpenKeyExW(user,VX_STATE_KEY,REG_OPTION_OPEN_LINK,KEY_READ|KEY_WRITE,&c->key);
	if(RegCloseKey(user)!=ERROR_SUCCESS) return FALSE;
	return e==ERROR_SUCCESS || e==ERROR_FILE_NOT_FOUND;
}
static BOOL file_owned(VX_CONTEXT *c,const REMOVE_PLAN *p,DWORD directory,const WCHAR *name,
	const WCHAR *path,BOOL exclusive)
{
	BOOL hashed=directory==REMOVE_ROOT || (directory==REMOVE_STATE && cache_name(name));
	BOOL recheck_shortcut=exclusive && directory==REMOVE_SHORTCUTS;
	if(!exclusive && directory==REMOVE_STATE && !wcscmp(name,L"host.lock")) return TRUE;
	DWORD access=recheck_shortcut?GENERIC_READ:exclusive?GENERIC_READ|DELETE:hashed?GENERIC_READ:FILE_READ_ATTRIBUTES;
	DWORD share=exclusive || hashed?FILE_SHARE_READ:FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE;
	HANDLE file=vx_file(path,FALSE,access,share,OPEN_EXISTING);
	if(file==INVALID_HANDLE_VALUE) return GetLastError()==ERROR_FILE_NOT_FOUND;
	BOOL ok=TRUE; char actual[65],want[65]; BOOL exists=FALSE;
	if(directory==REMOVE_ROOT) {
		/* Exact registered media/root ownership permits deletion of damaged
		 * known files; vx_file still rejects reparse points and hardlinks.
		 */
		ok=known_file(directory,name);
	} else if(directory==REMOVE_STATE && cache_name(name)) {
		WCHAR key[96];
		if(!wcscmp(name,L"setup.exe")) ok=vx_ascii(want,65,p->owner);
		else ok=swprintf(key,ARRAYSIZE(key),L"Cache.%ls",name)>0 &&
			vx_binary(c,key,want,sizeof(want),&exists) && exists && vx_hex(want,64);
		ok=ok && vx_hash(file,actual) && !strcmp(actual,want);
	} else if(directory==REMOVE_SHORTCUTS) {
		ok=setup_shortcut_owned(c,name,path);
		if(recheck_shortcut) {
			BY_HANDLE_FILE_INFORMATION before,after;
			ok=ok && GetFileInformationByHandle(file,&before) && vx_hash(file,actual);
			BOOL closed=CloseHandle(file); file=INVALID_HANDLE_VALUE;
			if(!ok || !closed) return FALSE;
			/* Shell Link's read open does not share DELETE. After its read-only
			 * ownership check, re-acquire delete access and revalidate ID/bytes.
			 * Existing ancestor pins remain; this gap is not atomic provenance.
			 */
			file=vx_file(path,FALSE,GENERIC_READ|DELETE,FILE_SHARE_READ,OPEN_EXISTING);
			if(file==INVALID_HANDLE_VALUE) return FALSE;
			ok=GetFileInformationByHandle(file,&after) &&
				before.dwVolumeSerialNumber==after.dwVolumeSerialNumber &&
				before.nFileIndexHigh==after.nFileIndexHigh && before.nFileIndexLow==after.nFileIndexLow &&
				vx_hash(file,want) && !strcmp(actual,want);
		}
	}
	if(!CloseHandle(file)) ok=FALSE;
	return ok;
}
static BOOL inventory(MSIHANDLE h,VX_CONTEXT *c,REMOVE_PLAN *p,BOOL collect,BOOL exclusive)
{
	WCHAR paths[REMOVE_DIRS][MAX_PATH],pattern[MAX_PATH],path[MAX_PATH];
	if(!directories(c,paths)) return FALSE;
	for(DWORD d=0;d<REMOVE_DIRS;++d) {
		wcscpy(path,paths[d]);
		unsigned int first=c->count; BOOL exists=FALSE,ok=pin_existing(c,paths[d],&exists);
		HANDLE find=INVALID_HANDLE_VALUE; WIN32_FIND_DATAW entry;
		if(ok && exists && d==REMOVE_SHORTCUTS) {
			const WCHAR *names[]={L"VX Bridge.lnk",L"VX Bridge Setup.lnk",L"VX Bridge Diagnostics.lnk"};
			for(unsigned int i=0;ok && i<ARRAYSIZE(names);++i)
				ok=(collect?add_file(p,d,names[i]):contains(p,d,names[i])) &&
					vx_path(path,paths[d],names[i]) && file_owned(c,p,d,names[i],path,exclusive);
		} else if(ok && exists) {
			ok=vx_path(pattern,paths[d],L"*");
			if(ok) {
				find=FindFirstFileW(pattern,&entry);
				if(find==INVALID_HANDLE_VALUE) ok=GetLastError()==ERROR_FILE_NOT_FOUND;
			}
			if(ok && find!=INVALID_HANDLE_VALUE) do {
				if(!wcscmp(entry.cFileName,L".") || !wcscmp(entry.cFileName,L"..")) continue;
				if(!vx_path(path,paths[d],entry.cFileName)) { ok=FALSE; break; }
				if(d==REMOVE_LOCAL && (!wcscmp(entry.cFileName,L"installer") || !wcscmp(entry.cFileName,L"logs"))) {
					if(!(entry.dwFileAttributes&FILE_ATTRIBUTE_DIRECTORY) || entry.dwFileAttributes&FILE_ATTRIBUTE_REPARSE_POINT)
						ok=FALSE;
					continue;
				}
				ok=!(entry.dwFileAttributes&(FILE_ATTRIBUTE_DIRECTORY|FILE_ATTRIBUTE_REPARSE_POINT)) &&
					(collect?add_file(p,d,entry.cFileName):contains(p,d,entry.cFileName)) &&
					vx_path(path,paths[d],entry.cFileName) &&
					file_owned(c,p,d,entry.cFileName,path,exclusive);
				if(!ok) break;
			} while(FindNextFileW(find,&entry));
			if(ok && find!=INVALID_HANDLE_VALUE && GetLastError()!=ERROR_NO_MORE_FILES) ok=FALSE;
		}
		if(find!=INVALID_HANDLE_VALUE && !FindClose(find)) ok=FALSE;
		if(!unpin(c,first)) ok=FALSE;
		if(!ok) { problem(h,path); return FALSE; }
	}
	return TRUE;
}
static BOOL row(MSIHANDLE db,const WCHAR *sql,MSIHANDLE record)
{
	MSIHANDLE view=0;
	BOOL ok=MsiDatabaseOpenViewW(db,sql,&view)==ERROR_SUCCESS &&
		MsiViewExecute(view,record)==ERROR_SUCCESS;
	if(view && MsiCloseHandle(view)!=ERROR_SUCCESS) ok=FALSE;
	return ok;
}
static BOOL registry_row(MSIHANDLE db,DWORD serial,const WCHAR *key,const WCHAR *name)
{
	WCHAR id[48]; MSIHANDLE r=MsiCreateRecord(6);
	BOOL ok=r && swprintf(id,ARRAYSIZE(id),L"VxRemoveRegistry%lu",(unsigned long)serial)>0 &&
		MsiRecordSetStringW(r,1,id)==ERROR_SUCCESS && MsiRecordSetInteger(r,2,1)==ERROR_SUCCESS &&
		MsiRecordSetStringW(r,3,key)==ERROR_SUCCESS && MsiRecordSetStringW(r,4,name)==ERROR_SUCCESS &&
		MsiRecordSetStringW(r,6,L"Maintenance")==ERROR_SUCCESS &&
		row(db,L"INSERT INTO `Registry` (`Registry`,`Root`,`Key`,`Name`,`Value`,`Component_`) VALUES (?,?,?,?,?,?) TEMPORARY",r);
	if(r && MsiCloseHandle(r)!=ERROR_SUCCESS) ok=FALSE;
	return ok;
}
static BOOL registry_inventory(MSIHANDLE h,VX_CONTEXT *c,const REMOVE_PLAN *p,MSIHANDLE db)
{
	const WCHAR *keys[]={VX_STATE_KEY,L"Software\\VxInfiniteTeesBridge\\MSI",
		L"Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\VxInfiniteTeesBridge"};
	HKEY user=NULL,key=NULL; BOOL ok=FALSE; DWORD serial=0; const WCHAR *failure=VX_STATE_KEY;
	if(RegOpenCurrentUser(KEY_READ|KEY_WRITE,&user)!=ERROR_SUCCESS) return FALSE;
	for(unsigned int k=0;k<ARRAYSIZE(keys);++k) {
		failure=keys[k];
		LSTATUS e=RegOpenKeyExW(user,keys[k],REG_OPTION_OPEN_LINK,KEY_READ,&key);
		if(e==ERROR_FILE_NOT_FOUND) continue;
		DWORD children=0;
		if(e!=ERROR_SUCCESS || RegQueryInfoKeyW(key,NULL,NULL,NULL,&children,NULL,NULL,NULL,NULL,NULL,NULL,NULL)!=ERROR_SUCCESS || children) goto done;
		for(DWORD index=0;;++index) {
			WCHAR name[96]; BYTE value[sizeof(VX_PENDING)]; DWORD n=ARRAYSIZE(name),size=sizeof(value),kind=0;
			e=RegEnumValueW(key,index,name,&n,NULL,&kind,value,&size);
			if(e==ERROR_NO_MORE_ITEMS) break;
			if(e!=ERROR_SUCCESS || index>=64) goto done;
			BOOL known=FALSE;
			if(k==0) {
				if(!wcscmp(name,L"Receipt")) known=kind==REG_BINARY && size==sizeof(VX_RECEIPT) &&
					p->had_receipt && !memcmp(value,&p->original,size);
				else if(!wcscmp(name,L"Pending")) {
					const VX_PENDING *pending=(const VX_PENDING *)value;
					known=p->had_pending && kind==REG_BINARY && size==sizeof(VX_PENDING) && vx_pending(pending) &&
						!memcmp(pending->nonce,p->startup.nonce,sizeof(pending->nonce)) &&
						!memcmp(&pending->after,&p->startup.after,sizeof(pending->after));
				}
				else if(!wcscmp(name,L"SourcePackage")) {
					WCHAR package[39],file[48],path[MAX_PATH];
					const VX_RECEIPT *source=p->had_receipt?&p->original:&p->receipt;
					known=vx_wide(package,39,source->package) && swprintf(file,48,L"%ls.msi",package)>0 &&
						vx_path(path,c->state,file) && kind==REG_SZ && size==(wcslen(path)+1)*sizeof(WCHAR) &&
						!memcmp(value,path,size);
					if(!known && p->had_pending)
						known=vx_wide(package,39,p->startup.after.package) && swprintf(file,48,L"%ls.msi",package)>0 &&
							vx_path(path,c->state,file) && kind==REG_SZ && size==(wcslen(path)+1)*sizeof(WCHAR) &&
							!memcmp(value,path,size);
				} else if(!wcsncmp(name,L"Cache.",6))
					known=cache_name(name+6) && kind==REG_BINARY && size==65 && vx_hex((char *)value,64);
			} else if(k==1) {
				const WCHAR *ids[]={L"VxGUI",L"VxControl",L"VxLicense",L"VxNotices",L"VxManifest",L"VxSums",L"Shortcut"};
				for(unsigned int i=0;i<ARRAYSIZE(ids);++i) if(!wcscmp(name,ids[i])) {
					WCHAR revision[41]; DWORD one=1;
					known=i==VX_COUNT?kind==REG_DWORD && size==sizeof(one) && !memcmp(value,&one,size):
						vx_wide(revision,41,p->receipt.revision) && kind==REG_SZ &&
						size==sizeof(revision) && !memcmp(value,revision,size);
				}
			} else {
				WCHAR expected[MAX_PATH+32],image[MAX_PATH];
				if(!wcscmp(name,L"DisplayName")) wcscpy(expected,L"VX Bridge");
				else if(!wcscmp(name,L"DisplayVersion")) {
					if(!vx_wide(expected,ARRAYSIZE(expected),p->receipt.version)) goto done;
				} else if(!wcscmp(name,L"UninstallString") || !wcscmp(name,L"ModifyPath")) {
					if(!vx_path(image,c->state,L"setup.exe") ||
						swprintf(expected,ARRAYSIZE(expected),L"\"%ls\" /%ls",image,
							!wcscmp(name,L"ModifyPath")?L"maintain":L"remove")<=0) goto done;
				} else goto done;
				known=kind==REG_SZ && size==(wcslen(expected)+1)*sizeof(WCHAR) && !memcmp(value,expected,size);
			}
			if(!known || (db && !registry_row(db,serial++,keys[k],name))) goto done;
		}
		if(RegCloseKey(key)!=ERROR_SUCCESS) { key=NULL; goto done; }
		key=NULL;
	}
	/* No blanket subtree deletion, including on an otherwise empty app key. */
	failure=L"Software\\VxInfiniteTeesBridge";
	LSTATUS parent=RegOpenKeyExW(user,L"Software\\VxInfiniteTeesBridge",REG_OPTION_OPEN_LINK,KEY_READ,&key);
	if(parent!=ERROR_SUCCESS && parent!=ERROR_FILE_NOT_FOUND) goto done;
	if(parent==ERROR_SUCCESS) {
		DWORD values=0;
		if(RegQueryInfoKeyW(key,NULL,NULL,NULL,NULL,NULL,NULL,&values,NULL,NULL,NULL,NULL)!=ERROR_SUCCESS || values) goto done;
		for(DWORD i=0;;++i) {
			WCHAR name[96]; DWORD n=ARRAYSIZE(name);
			LSTATUS e=RegEnumKeyExW(key,i,name,&n,NULL,NULL,NULL,NULL);
			if(e==ERROR_NO_MORE_ITEMS) break;
			if(e!=ERROR_SUCCESS || (wcscmp(name,L"Installer") && wcscmp(name,L"MSI"))) goto done;
		}
	}
	ok=TRUE;
done:
	if(key && RegCloseKey(key)!=ERROR_SUCCESS) ok=FALSE;
	if(user && RegCloseKey(user)!=ERROR_SUCCESS) ok=FALSE;
	if(!ok) problem(h,failure);
	return ok;
}
static BOOL encode(MSIHANDLE h,const WCHAR *action,const REMOVE_PLAN *p)
{
	static const WCHAR hex[]=L"0123456789abcdef";
	WCHAR *text=HeapAlloc(GetProcessHeap(),0,(2*sizeof(*p)+1)*sizeof(WCHAR));
	if(!text) return FALSE;
	for(size_t i=0;i<sizeof(*p);++i) {
		BYTE b=((const BYTE *)p)[i]; text[2*i]=hex[b>>4]; text[2*i+1]=hex[b&15];
	}
	text[2*sizeof(*p)]=0;
	BOOL ok=MsiSetPropertyW(h,action,text)==ERROR_SUCCESS;
	HeapFree(GetProcessHeap(),0,text); return ok;
}
static BOOL decode(MSIHANDLE h,REMOVE_PLAN *p)
{
	WCHAR *text=HeapAlloc(GetProcessHeap(),0,(2*sizeof(*p)+1)*sizeof(WCHAR));
	if(!text) return FALSE;
	BOOL ok=setup_property(h,L"CustomActionData",text,2*sizeof(*p)+1) && wcslen(text)==2*sizeof(*p);
	for(size_t i=0;ok && i<sizeof(*p);++i) {
		BYTE b=0;
		for(unsigned int j=0;j<2;++j) {
			WCHAR ch=text[2*i+j];
			if(ch>=L'0' && ch<=L'9') b=(BYTE)(16*b+ch-L'0');
			else if(ch>=L'a' && ch<=L'f') b=(BYTE)(16*b+ch-L'a'+10);
			else { ok=FALSE; break; }
		}
		((BYTE *)p)[i]=b;
	}
	HeapFree(GetProcessHeap(),0,text);
	ok=ok && p->size==sizeof(*p) && p->count<=REMOVE_MAX_FILES && p->quiet<=1 &&
		p->had_receipt<=1 && p->had_pending<=1 && (!p->had_receipt || vx_receipt(&p->original)) &&
		p->sid[255]==0 && vx_receipt(&p->receipt) && p->owner[64]==0 &&
		p->startup.run_saved<=1 && p->startup.run_changed<=1 && p->startup.run_command[260]==0;
	char owner[65]; ok=ok && vx_ascii(owner,65,p->owner) && vx_hex(owner,64);
	for(DWORD i=0;ok && i<p->count;++i)
		ok=p->files[i].name[95]==0 && known_file(p->files[i].directory,p->files[i].name);
	return ok;
}
static BOOL session_user(MSIHANDLE h,VX_CONTEXT *c,const REMOVE_PLAN *p)
{
	WCHAR product[39],actual[256],expected[39];
	return !wcscmp(c->sid,p->sid) && vx_wide(expected,39,p->receipt.product) &&
		setup_property(h,L"ProductCode",product,39) && !wcscmp(product,expected) &&
		setup_property(h,L"UserSID",actual,256) && !wcscmp(actual,c->sid);
}
static BOOL file_rows(MSIHANDLE h,MSIHANDLE db,VX_CONTEXT *c,const REMOVE_PLAN *p)
{
	WCHAR paths[REMOVE_DIRS][MAX_PATH],property[48],id[48];
	if(!directories(c,paths)) return FALSE;
	for(DWORD d=0;d<REMOVE_DIRS;++d) {
		WCHAR value[MAX_PATH];
		if(swprintf(property,48,L"VX_REMOVE_DIR%lu",(unsigned long)d)<=0 ||
			swprintf(value,MAX_PATH,L"%ls\\",paths[d])<=0 ||
			MsiSetPropertyW(h,property,value)!=ERROR_SUCCESS) return FALSE;
	}
	for(DWORD i=0;i<p->count;++i) {
		if(p->files[i].directory==REMOVE_ROOT || p->files[i].directory==REMOVE_SHORTCUTS) continue;
		MSIHANDLE r=MsiCreateRecord(5);
		BOOL ok=r && swprintf(property,48,L"VX_REMOVE_DIR%lu",(unsigned long)p->files[i].directory)>0 &&
			swprintf(id,48,L"VxRemoveFile%lu",(unsigned long)i)>0 &&
			MsiRecordSetStringW(r,1,id)==ERROR_SUCCESS && MsiRecordSetStringW(r,2,L"Maintenance")==ERROR_SUCCESS &&
			MsiRecordSetStringW(r,3,p->files[i].name)==ERROR_SUCCESS &&
			MsiRecordSetStringW(r,4,property)==ERROR_SUCCESS && MsiRecordSetInteger(r,5,2)==ERROR_SUCCESS &&
			row(db,L"INSERT INTO `RemoveFile` (`FileKey`,`Component_`,`FileName`,`DirProperty`,`InstallMode`) VALUES (?,?,?,?,?) TEMPORARY",r);
		if(r && MsiCloseHandle(r)!=ERROR_SUCCESS) ok=FALSE;
		if(!ok) return FALSE;
	}
	return !p->startup.run_saved ||
		registry_row(db,100,L"Software\\Microsoft\\Windows\\CurrentVersion\\Run",L"VxInfiniteTeesBridge");
}
static BOOL removal_media(MSIHANDLE h,VX_CONTEXT *c,const VX_RECEIPT *r)
{
	WCHAR product[39],path[MAX_PATH],package[39]; DWORD n=MAX_PATH; MSIHANDLE summary=0;
	if(!vx_wide(product,39,r->product) || !vx_wide(package,39,r->package)) return FALSE;
	UINT code=MsiGetProductInfoExW(product,c->sid,MSIINSTALLCONTEXT_USERUNMANAGED,INSTALLPROPERTY_LOCALPACKAGE,path,&n);
	if(code!=ERROR_SUCCESS && code!=ERROR_UNKNOWN_PROPERTY) return FALSE;
	/* Conventional removal may run from the verified original MSI when
	 * interrupted rollback left no usable OS LocalPackage.
	 */
	BOOL original=code!=ERROR_SUCCESS;
	if(!original && GetFileAttributesW(path)==INVALID_FILE_ATTRIBUTES) {
		if(!missing(GetLastError())) return FALSE;
		original=TRUE;
	}
	if(original && !setup_property(h,L"OriginalDatabase",path,MAX_PATH)) return FALSE;
	HANDLE file=vx_file(path,FALSE,GENERIC_READ,FILE_SHARE_READ,OPEN_EXISTING);
	if(file==INVALID_HANDLE_VALUE) return FALSE;
	BOOL ok=MsiGetSummaryInformationW(0,path,0,&summary)==ERROR_SUCCESS;
	if(original) {
		WCHAR key[96]; char expected[65],actual[65]; BOOL exists=FALSE;
		ok=ok && swprintf(key,ARRAYSIZE(key),L"Cache.%ls.msi",package)>0 &&
			vx_binary(c,key,expected,sizeof(expected),&exists) && exists && !expected[64] && vx_hex(expected,64) &&
			vx_hash(file,actual) && !strcmp(expected,actual);
	}
	WCHAR value[39]; UINT type=0; int integer=0; FILETIME time={0}; n=39;
	ok=ok && MsiSummaryInfoGetPropertyW(summary,PIDSI_REVNUMBER,&type,&integer,&time,value,&n)==ERROR_SUCCESS &&
		type==VT_LPSTR && !wcscmp(value,package);
	if(summary && MsiCloseHandle(summary)!=ERROR_SUCCESS) ok=FALSE;
	if(!CloseHandle(file)) ok=FALSE;
	return ok;
}
static BOOL maintenance_properties(MSIHANDLE h,VX_CONTEXT *c,const VX_RECEIPT *r)
{
	WCHAR package[39],name[48],source[MAX_PATH],owner[MAX_PATH];
	return vx_wide(package,39,r->package) && swprintf(name,48,L"%ls.msi",package)>0 &&
		vx_path(source,c->state,name) && vx_path(owner,c->state,L"setup.exe") &&
		MsiSetPropertyW(h,L"VX_CACHE_SOURCE",source)==ERROR_SUCCESS &&
		MsiSetPropertyW(h,L"VX_MAINTENANCE_EXE",owner)==ERROR_SUCCESS;
}
UINT setup_remove_authorize(MSIHANDLE h)
{
	VX_CONTEXT c; MSIHANDLE db=0; BOOL ok=FALSE; WCHAR root[MAX_PATH],product[39],level[16]; HANDLE lock=NULL;
	REMOVE_PLAN *p=HeapAlloc(GetProcessHeap(),HEAP_ZERO_MEMORY,sizeof(*p));
	if(!p) return problem(h,L"removal plan allocation");
	if(!vx_context(&c)) { HeapFree(GetProcessHeap(),0,p); return problem(h,L"current user"); }
	INSTALLSTATE installed,action;
	if(!(lock=owner(&c)) || !(db=MsiGetActiveDatabase(h)) || !setup_receipt(db,&p->receipt,p->owner) ||
		!setup_catalog_owned(&c,&p->receipt,&p->receipt) ||
		!setup_property(h,L"INSTALLDIR",root,MAX_PATH)) goto done;
	size_t n=wcslen(root); if(n && root[n-1]==L'\\') root[n-1]=0;
	if(_wcsicmp(root,c.root) || MsiGetFeatureStateW(h,L"Bridge",&installed,&action)!=ERROR_SUCCESS ||
		(installed!=INSTALLSTATE_LOCAL && installed!=INSTALLSTATE_ADVERTISED && installed!=INSTALLSTATE_ABSENT) ||
		action!=INSTALLSTATE_ABSENT ||
		!vx_wide(product,39,p->receipt.product) ||
		!setup_property(h,L"ProductCode",root,MAX_PATH) || wcscmp(root,product) ||
		!setup_property(h,L"UserSID",root,MAX_PATH) || wcscmp(root,c.sid) ||
		!setup_property(h,L"UILevel",level,16) || !open_state(&c) || !removal_media(h,&c,&p->receipt)) goto done;
	p->size=sizeof(*p); p->quiet=!wcscmp(level,L"2"); wcscpy(p->sid,c.sid);
	BOOL exists=FALSE; VX_RECEIPT before; VX_PENDING pending;
	if(!vx_binary(&c,L"Receipt",&before,sizeof(before),&exists) || (exists && !vx_receipt(&before))) goto done;
	p->had_receipt=exists;
	if(exists) p->original=before;
	if(!vx_binary(&c,L"Pending",&pending,sizeof(pending),&exists) ||
		(exists && (!vx_pending(&pending) ||
			(memcmp(&pending.after,&p->receipt,sizeof(p->receipt)) &&
				(!pending.has_before || memcmp(&pending.before,&p->receipt,sizeof(p->receipt)))) ||
			(p->had_receipt && (!pending.has_before || memcmp(&pending.before,&before,sizeof(before))))))) goto done;
	p->had_pending=exists;
	if(exists) p->startup=pending;
	else if(p->had_receipt && memcmp(&before,&p->receipt,sizeof(before))) goto done;
	if(p->had_pending && pending.has_before && !memcmp(&pending.before,&p->receipt,sizeof(p->receipt)) &&
		strcmp(pending.before.product,pending.after.product) &&
		vx_registered(&c,&pending.before,FALSE) && vx_registered(&c,&pending.after,FALSE)) {
		if(!setup_startup(&c,&p->startup,0,FALSE,TRUE) || !fixed_files(p) ||
			!inventory(h,&c,p,TRUE,FALSE) || !registry_inventory(h,&c,p,0)) goto done;
		WCHAR path[MAX_PATH];
		if(!vx_path(path,c.state,L"host.lock")) goto done;
		HANDLE lease=vx_file(path,FALSE,GENERIC_READ|GENERIC_WRITE,0,OPEN_EXISTING);
		if(lease==INVALID_HANDLE_VALUE && !missing(GetLastError())) goto done;
		if(lease!=INVALID_HANDLE_VALUE && !CloseHandle(lease)) goto done;
		/* A fully installed old client can be removed conventionally without
		 * wiping the new version's shared resources or user settings. This
		 * intermediate step keeps Pending and is not completed recovery.
		 */
		ok=maintenance_properties(h,&c,&p->receipt) && MsiSetPropertyW(h,L"VX_PRESERVE_MODE",L"1")==ERROR_SUCCESS;
		goto done;
	}
	if(!setup_startup(&c,&p->startup,0,FALSE,TRUE) ||
		!fixed_files(p) || !inventory(h,&c,p,TRUE,FALSE) || !registry_inventory(h,&c,p,db) ||
		!maintenance_properties(h,&c,&p->receipt) || !file_rows(h,db,&c,p)) goto done;
	const WCHAR *actions[]={L"VxRemoveBegin",L"VxRemoveUndo",L"VxRemoveFinish"};
	for(unsigned int i=0;i<ARRAYSIZE(actions);++i) if(!encode(h,actions[i],p)) goto done;
	ok=MsiSetPropertyW(h,L"VX_REMOVE_MODE",L"1")==ERROR_SUCCESS;
done:
	if(db && MsiCloseHandle(db)!=ERROR_SUCCESS) ok=FALSE;
	if(lock && !close_owner(lock)) ok=FALSE;
	if(!vx_close(&c)) ok=FALSE;
	HeapFree(GetProcessHeap(),0,p);
	return ok?ERROR_SUCCESS:problem(h,L"registered per-user package / exact owned inventory");
}
typedef struct { MSIHANDLE session; BOOL quiet; } REMOVE_WAIT;
static BOOL wait_quit(HANDLE process,void *data)
{
	REMOVE_WAIT *w=data; DWORD wait=WaitForSingleObject(process,w->quiet?3000:1000);
	if(wait==WAIT_OBJECT_0) return TRUE;
	if(wait!=WAIT_TIMEOUT || w->quiet) return FALSE;
	MSIHANDLE r=MsiCreateRecord(0);
	if(!r) return FALSE;
	BOOL ok=MsiRecordSetStringW(r,0,L"Disconnect normally in ProTee Labs, then click OK to continue VX Bridge uninstall. "
		L"Quit is pending normal source departure. Cancel cannot withdraw that request. No process will be force-closed.")==ERROR_SUCCESS;
	int choice=ok?MsiProcessMessage(w->session,(INSTALLMESSAGE)(INSTALLMESSAGE_USER|MB_OKCANCEL|MB_ICONINFORMATION),r):IDCANCEL;
	if(MsiCloseHandle(r)!=ERROR_SUCCESS) return FALSE;
	return choice==IDOK && WaitForSingleObject(process,120000)==WAIT_OBJECT_0;
}
__declspec(dllexport) UINT __stdcall VxRemoveBegin(MSIHANDLE h)
{
	VX_DIAG_SPAN span=setup_diag_session_start(h,"remove_begin");
	REMOVE_PLAN *p=HeapAlloc(GetProcessHeap(),HEAP_ZERO_MEMORY,sizeof(*p));
	VX_CONTEXT c; BOOL ok=FALSE,held=FALSE; HANDLE lease=INVALID_HANDLE_VALUE,lock=NULL;
	const WCHAR *failure=L"deferred mode / exact plan / actual session / owner";
	if(!p) return problem(h,L"removal plan allocation");
	if(!vx_context(&c)) { HeapFree(GetProcessHeap(),0,p); return problem(h,L"current user"); }
	/* InstallInitialize may already unpublish the product before this script
	 * runs. Registered/cache admission was checked by the immediate authorizer;
	 * retain the actual deferred session, exact plan and owned-state checks.
	 */
	if(!(lock=owner(&c)) || !MsiGetMode(h,MSIRUNMODE_SCHEDULED) || !decode(h,p) || !session_user(h,&c,p) ||
		!vx_enter(&c)) goto done;
	held=TRUE;
	failure=L"existing owned registry state";
	if(!open_state(&c)) goto done;
	VX_RECEIPT actual; VX_PENDING pending; BOOL exists=FALSE;
	failure=L"exact Pending / original Receipt";
	if(!vx_binary(&c,L"Pending",&pending,sizeof(pending),&exists) || !!exists!=!!p->had_pending ||
		!vx_binary(&c,L"Receipt",&actual,sizeof(actual),&exists) ||
		!!exists!=!!p->had_receipt || (exists && memcmp(&actual,&p->original,sizeof(actual)))) goto done;
	failure=L"exact owned registry inventory";
	if(!registry_inventory(h,&c,p,0)) goto done;
	/* Durable local denial; ordinary starts still never call Windows Installer.
	 * Rollback restores the verified original receipt, not another package's.
	 */
	failure=L"local denial Receipt removal";
	if(exists && !vx_delete(&c,L"Receipt")) goto done;
	failure=L"admission mutex release";
	if(!vx_leave(&c)) goto done;
	held=FALSE;
	WCHAR path[MAX_PATH];
	failure=L"source-safe Quit / owned host lease";
	if(!vx_path(path,c.state,L"host.lock")) goto done;
	lease=vx_file(path,FALSE,GENERIC_READ|GENERIC_WRITE,0,OPEN_EXISTING);
	if(lease==INVALID_HANDLE_VALUE && !missing(GetLastError())) {
		REMOVE_WAIT wait={h,p->quiet};
		if(!setup_owned_quit(&c,&p->receipt,wait_quit,&wait)) goto done;
		lease=vx_file(path,FALSE,GENERIC_READ|GENERIC_WRITE,0,OPEN_EXISTING);
		if(lease==INVALID_HANDLE_VALUE) goto done;
	}
	if(lease!=INVALID_HANDLE_VALUE) {
		if(!CloseHandle(lease)) { lease=INVALID_HANDLE_VALUE; goto done; }
		lease=INVALID_HANDLE_VALUE;
	}
	VX_PENDING startup={0};
	failure=L"exclusive owned file inventory";
	if(!inventory(h,&c,p,FALSE,TRUE)) goto done;
	failure=L"unchanged owned startup fixture";
	ok=setup_startup(&c,&startup,0,FALSE,TRUE) &&
		startup.run_saved==p->startup.run_saved && !wcscmp(startup.run_command,p->startup.run_command);
done:
	if(lease!=INVALID_HANDLE_VALUE && !CloseHandle(lease)) ok=FALSE;
	if(lock && !close_owner(lock)) ok=FALSE;
	if(held && !vx_leave(&c)) ok=FALSE;
	if(!vx_close(&c)) ok=FALSE;
	HeapFree(GetProcessHeap(),0,p);
	setup_diag_session_end(h,span,"MSI",ok?ERROR_SUCCESS:ERROR_INSTALL_FAILURE);
	return ok?ERROR_SUCCESS:problem(h,failure);
}
__declspec(dllexport) UINT __stdcall VxRemoveUndo(MSIHANDLE h)
{
	VX_DIAG_SPAN span=setup_diag_session_start(h,"remove_undo");
	REMOVE_PLAN *p=HeapAlloc(GetProcessHeap(),HEAP_ZERO_MEMORY,sizeof(*p));
	VX_CONTEXT c; BOOL ok=FALSE,held=FALSE; HANDLE lock=NULL;
	if(!p) return problem(h,L"rollback plan allocation");
	if(!vx_context(&c)) { HeapFree(GetProcessHeap(),0,p); return problem(h,L"rollback user"); }
	if(!(lock=owner(&c)) || !MsiGetMode(h,MSIRUNMODE_ROLLBACK) || !decode(h,p) || !session_user(h,&c,p) ||
		!vx_enter(&c)) goto done;
	held=TRUE;
	if(!p->had_receipt) { ok=TRUE; goto done; }
	if(!open_state(&c) || !c.key || !registry_inventory(h,&c,p,0)) goto done;
	VX_RECEIPT actual; BOOL exists=FALSE; VX_PENDING pending;
	if(!vx_binary(&c,L"Pending",&pending,sizeof(pending),&exists) || !!exists!=!!p->had_pending ||
		!vx_binary(&c,L"Receipt",&actual,sizeof(actual),&exists) ||
		(exists && memcmp(&actual,&p->original,sizeof(actual)))) goto done;
	ok=inventory(h,&c,p,FALSE,FALSE) &&
		(exists || vx_put(&c,L"Receipt",&p->original,sizeof(p->original)));
done:
	if(held && !vx_leave(&c)) ok=FALSE;
	if(lock && !close_owner(lock)) ok=FALSE;
	if(!vx_close(&c)) ok=FALSE;
	HeapFree(GetProcessHeap(),0,p);
	setup_diag_session_end(h,span,"MSI",ok?ERROR_SUCCESS:ERROR_INSTALL_FAILURE);
	return ok?ERROR_SUCCESS:problem(h,L"rollback receipt restoration; Repair required");
}
static BOOL empty_key(HKEY user,const WCHAR *path)
{
	HKEY key=NULL; LSTATUS e=RegOpenKeyExW(user,path,REG_OPTION_OPEN_LINK,KEY_READ,&key);
	if(e==ERROR_FILE_NOT_FOUND) return TRUE;
	DWORD children=0,values=0;
	BOOL ok=e==ERROR_SUCCESS &&
		RegQueryInfoKeyW(key,NULL,NULL,NULL,&children,NULL,NULL,&values,NULL,NULL,NULL,NULL)==ERROR_SUCCESS &&
		!children && !values;
	if(key && RegCloseKey(key)!=ERROR_SUCCESS) ok=FALSE;
	return ok && RegDeleteKeyW(user,path)==ERROR_SUCCESS;
}
static BOOL remove_directory(VX_CONTEXT *c,const WCHAR *path)
{
	unsigned int first=c->count; BOOL exists=FALSE;
	if(!pin_existing(c,path,&exists)) { unpin(c,first); return FALSE; }
	if(!exists) return unpin(c,first);
	/* Release this directory's no-delete pin, retaining its checked ancestors.
	 * The deletion itself uses a validated, non-reparse, empty directory handle.
	 */
	BY_HANDLE_FILE_INFORMATION before,after;
	if(c->count==first || !GetFileInformationByHandle(c->pins[c->count-1],&before) ||
		!CloseHandle(c->pins[--c->count])) { unpin(c,first); return FALSE; }
	HANDLE directory=vx_file(path,TRUE,FILE_READ_ATTRIBUTES|DELETE,FILE_SHARE_READ,OPEN_EXISTING);
	BOOL ok=directory!=INVALID_HANDLE_VALUE; FILE_DISPOSITION_INFO disposition={TRUE};
	if(ok) ok=GetFileInformationByHandle(directory,&after) &&
		before.dwVolumeSerialNumber==after.dwVolumeSerialNumber &&
		before.nFileIndexHigh==after.nFileIndexHigh && before.nFileIndexLow==after.nFileIndexLow &&
		SetFileInformationByHandle(directory,FileDispositionInfo,&disposition,sizeof(disposition));
	if(directory!=INVALID_HANDLE_VALUE && !CloseHandle(directory)) ok=FALSE;
	if(!unpin(c,first)) ok=FALSE;
	return ok && GetFileAttributesW(path)==INVALID_FILE_ATTRIBUTES && missing(GetLastError());
}
static BOOL absent(VX_CONTEXT *c)
{
	WCHAR paths[REMOVE_DIRS][MAX_PATH];
	if(!directories(c,paths)) return FALSE;
	for(unsigned int i=0;i<REMOVE_DIRS;++i) {
		unsigned int first=c->count; BOOL exists=FALSE;
		BOOL ok=pin_existing(c,paths[i],&exists);
		if(ok && i==REMOVE_SHORTCUTS && exists) {
			const WCHAR *names[]={L"VX Bridge.lnk",L"VX Bridge Setup.lnk",L"VX Bridge Diagnostics.lnk"}; WCHAR path[MAX_PATH];
			for(unsigned int j=0;ok && j<ARRAYSIZE(names);++j)
				ok=vx_path(path,paths[i],names[j]) && GetFileAttributesW(path)==INVALID_FILE_ATTRIBUTES && missing(GetLastError());
		} else if(i!=REMOVE_SHORTCUTS) ok=ok && !exists;
		if(!unpin(c,first)) ok=FALSE;
		if(!ok) return FALSE;
	}
	HKEY user=NULL,key=NULL;
	if(RegOpenCurrentUser(KEY_READ,&user)!=ERROR_SUCCESS) return FALSE;
	const WCHAR *keys[]={L"Software\\VxInfiniteTeesBridge",
		L"Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\VxInfiniteTeesBridge"};
	BOOL ok=TRUE;
	for(unsigned int i=0;i<ARRAYSIZE(keys);++i) {
		LSTATUS e=RegOpenKeyExW(user,keys[i],0,KEY_READ,&key);
		if(e==ERROR_SUCCESS) { RegCloseKey(key); key=NULL; ok=FALSE; }
		else if(e!=ERROR_FILE_NOT_FOUND) ok=FALSE;
	}
	if(RegCloseKey(user)!=ERROR_SUCCESS) ok=FALSE;
	VX_PENDING startup={0};
	return ok && setup_startup(c,&startup,0,FALSE,TRUE) && !startup.run_saved;
}
__declspec(dllexport) UINT __stdcall VxRemoveFinish(MSIHANDLE h)
{
	VX_DIAG_SPAN span=setup_diag_session_start(h,"remove_finish");
	REMOVE_PLAN *p=HeapAlloc(GetProcessHeap(),HEAP_ZERO_MEMORY,sizeof(*p));
	VX_CONTEXT c; BOOL ok=FALSE; HKEY user=NULL; HANDLE lock=NULL;
	if(!p) return problem(h,L"cleanup plan allocation");
	if(!vx_context(&c)) { HeapFree(GetProcessHeap(),0,p); return problem(h,L"cleanup user"); }
	if(!(lock=owner(&c)) || !MsiGetMode(h,MSIRUNMODE_SCHEDULED) || !decode(h,p) || !session_user(h,&c,p)) goto done;
	WCHAR paths[REMOVE_DIRS][MAX_PATH];
	if(!directories(&c,paths)) goto done;
	/* The exact authored null-name RemoveFile row must already have removed
	 * the app root. Never turn a failed standard cleanup into native success.
	 */
	unsigned int first=c.count; BOOL root_exists=FALSE;
	BOOL root_absent=pin_existing(&c,c.root,&root_exists) && !root_exists;
	if(!unpin(&c,first) || !root_absent) goto done;
	for(unsigned int i=REMOVE_STATE;i<REMOVE_SHORTCUTS;++i) if(!remove_directory(&c,paths[i])) { problem(h,paths[i]); goto done; }
	if(RegOpenCurrentUser(KEY_READ|KEY_WRITE,&user)!=ERROR_SUCCESS) goto done;
	const WCHAR *keys[]={VX_STATE_KEY,L"Software\\VxInfiniteTeesBridge\\MSI",L"Software\\VxInfiniteTeesBridge",
		L"Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\VxInfiniteTeesBridge"};
	for(unsigned int i=0;i<ARRAYSIZE(keys);++i) if(!empty_key(user,keys[i])) { problem(h,keys[i]); goto done; }
	ok=absent(&c);
done:
	if(user && RegCloseKey(user)!=ERROR_SUCCESS) ok=FALSE;
	if(lock && !close_owner(lock)) ok=FALSE;
	if(!vx_close(&c)) ok=FALSE;
	HeapFree(GetProcessHeap(),0,p);
	setup_diag_session_end(h,span,"MSI",ok?ERROR_SUCCESS:ERROR_INSTALL_FAILURE);
	return ok?ERROR_SUCCESS:problem(h,L"owned files, directories or registry remain");
}
UINT setup_remove_verify(MSIHANDLE h)
{
	VX_CONTEXT c; BOOL ok=FALSE;
	if(!vx_context(&c)) return problem(h,L"completion user");
	ok=!MsiGetMode(h,MSIRUNMODE_REBOOTATEND) && !MsiGetMode(h,MSIRUNMODE_REBOOTNOW) && absent(&c);
	if(!vx_close(&c)) ok=FALSE;
	return ok?ERROR_SUCCESS:problem(h,L"cleanup verification / reboot request");
}
