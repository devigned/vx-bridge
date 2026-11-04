/* SPDX-License-Identifier: MIT
 * COMPILE ONLY here. Future approved Windows execution touches invented
 * temporary directories/files only; no session, registry, pipe or bridge.
 */
#include "protocol.h"
#include <objbase.h>
#include <winioctl.h>
#include <stdio.h>

static BOOL fixture_file(const WCHAR *path)
{
	HANDLE file=CreateFileW(path,GENERIC_WRITE,0,NULL,CREATE_NEW,FILE_ATTRIBUTE_NORMAL,NULL);
	if(file==INVALID_HANDLE_VALUE) return FALSE;
	DWORD wrote=0; const char content[]="invented damaged payload fixture";
	BOOL ok=WriteFile(file,content,sizeof(content),&wrote,NULL) && wrote==sizeof(content);
	if(!CloseHandle(file)) ok=FALSE;
	return ok;
}
static BOOL junction(const WCHAR *path,const WCHAR *target)
{
	struct {
		DWORD tag;
		WORD length,reserved,sub_offset,sub_length,print_offset,print_length;
		WCHAR names[MAX_PATH*2+8];
	} data={0};
	WCHAR substitute[MAX_PATH+4];
	if(swprintf(substitute,ARRAYSIZE(substitute),L"\\??\\%ls",target)<=0 ||
		!CreateDirectoryW(path,NULL)) return FALSE;
	size_t sub=wcslen(substitute),print=wcslen(target);
	if(sub+print+2>ARRAYSIZE(data.names)) return FALSE;
	data.tag=IO_REPARSE_TAG_MOUNT_POINT;
	data.sub_length=(WORD)(sub*2); data.print_offset=(WORD)((sub+1)*2); data.print_length=(WORD)(print*2);
	data.length=(WORD)(8+(sub+print+2)*2);
	memcpy(data.names,substitute,(sub+1)*2);
	memcpy(data.names+sub+1,target,(print+1)*2);
	HANDLE directory=CreateFileW(path,GENERIC_WRITE,0,NULL,OPEN_EXISTING,
		FILE_FLAG_OPEN_REPARSE_POINT|FILE_FLAG_BACKUP_SEMANTICS,NULL);
	if(directory==INVALID_HANDLE_VALUE) return FALSE;
	DWORD returned=0;
	BOOL ok=DeviceIoControl(directory,FSCTL_SET_REPARSE_POINT,&data,data.length+8,NULL,0,&returned,NULL);
	if(!CloseHandle(directory)) ok=FALSE;
	return ok;
}
static BOOL private_context(VX_CONTEXT *c,const WCHAR *base)
{
	memset(c,0,sizeof(*c));
	if(wcslen(base)>=MAX_PATH) return FALSE;
	wcscpy(c->base,base);
	WCHAR parent[MAX_PATH];
	return vx_path(parent,base,L"Programs") && vx_path(c->root,parent,L"VxInfiniteTeesBridge");
}
int wmain(void)
{
	WCHAR temp[MAX_PATH],base[MAX_PATH],name[96],guid[39],parent[MAX_PATH],target[MAX_PATH],file[MAX_PATH],renamed[MAX_PATH];
	GUID id; VX_CONTEXT c; BOOL created=FALSE; unsigned int slot=(unsigned int)-1;
	if(!GetTempPathW(ARRAYSIZE(temp),temp) || !GetLongPathNameW(temp,temp,ARRAYSIZE(temp)) ||
		CoCreateGuid(&id)!=S_OK || StringFromGUID2(&id,guid,39)!=39 ||
		swprintf(name,ARRAYSIZE(name),L"VX-PRIVATE-root-%ls",guid)<=0) return 1;
	size_t n=wcslen(temp); if(n && temp[n-1]==L'\\') temp[n-1]=0;
	if(!vx_path(base,temp,name) || !CreateDirectoryW(base,NULL) ||
		!vx_path(parent,base,L"Programs") || !vx_path(target,base,L"invented-target") ||
		!private_context(&c,base)) return 1;
	/* Missing owned directory is created/pinned, not treated as an alias. */
	if(!setup_pin_root(&c,&created,&slot) || !created || !setup_root_pinned(&c) ||
		swprintf(renamed,ARRAYSIZE(renamed),L"%ls.moved",c.root)<=0) return 2;
	if(MoveFileW(c.root,renamed)) return 3;
	DWORD error=GetLastError();
	if(error!=ERROR_SHARING_VIOLATION && error!=ERROR_ACCESS_DENIED) return 4;
	if(MoveFileW(parent,target)) return 5;
	error=GetLastError();
	if(error!=ERROR_SHARING_VIOLATION && error!=ERROR_ACCESS_DENIED) return 6;
	HANDLE write_directory=CreateFileW(c.root,GENERIC_WRITE,
		FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE,NULL,OPEN_EXISTING,FILE_FLAG_BACKUP_SEMANTICS,NULL);
	if(write_directory!=INVALID_HANDLE_VALUE) { CloseHandle(write_directory); return 17; }
	error=GetLastError();
	if(error!=ERROR_SHARING_VIOLATION && error!=ERROR_ACCESS_DENIED) return 18;
	BY_HANDLE_FILE_INFORMATION original,wrong; unsigned int count=c.count,other=0;
	if(!setup_root_snapshot(&c,slot,&original) || setup_release_root(&c,&other,&original) ||
		c.count!=count || !setup_release_root(&c,&slot,&original) ||
		slot!=(unsigned int)-1 || c.count!=count-1) return 21;
	if(MoveFileW(parent,target)) return 22;
	if(!setup_repin_root(&c,&slot,&original) || MoveFileW(c.root,renamed)) return 23;
	if(!setup_release_root(&c,&slot,&original) || !RemoveDirectoryW(c.root) ||
		setup_repin_root(&c,&slot,&original) || GetFileAttributesW(c.root)!=INVALID_FILE_ATTRIBUTES ||
		GetLastError()!=ERROR_FILE_NOT_FOUND) return 24;
	if(!setup_recreate_root(&c,&slot,&original) || !setup_root_pinned(&c)) return 25;
	if(!setup_release_root(&c,&slot,&original) || setup_recreate_root(&c,&slot,&wrong) ||
		slot!=(unsigned int)-1 || c.count!=count-1) return 26;
	wrong=original; wrong.nFileIndexLow^=1;
	if(setup_repin_root(&c,&slot,&wrong) || !setup_repin_root(&c,&slot,&original)) return 27;
	if(!vx_path(file,c.root,L"GSPconnect.exe") || !fixture_file(file)) return 7;
	VX_RECEIPT receipt={0};
	if(!setup_payload(&c,&receipt,TRUE) || setup_payload(&c,&receipt,FALSE) || !DeleteFileW(file)) return 8;
	if(!setup_remove_root(&c,&slot) || GetFileAttributesW(c.root)!=INVALID_FILE_ATTRIBUTES ||
		GetLastError()!=ERROR_FILE_NOT_FOUND || !vx_close(&c)) return 9;
	/* Empty root junction: the b8cd enumeration-only preflight accepted this. */
	if(!CreateDirectoryW(target,NULL) || !junction(c.root,target) || !private_context(&c,base)) return 10;
	if(setup_pin_root(&c,&created,&slot) || !vx_close(&c) || !RemoveDirectoryW(c.root)) return 11;
	/* Programs junction must be refused before creating any child through it. */
	if(!RemoveDirectoryW(parent) || !junction(parent,target) || !private_context(&c,base)) return 12;
	if(setup_pin_root(&c,&created,&slot) || !vx_close(&c) ||
		!vx_path(file,target,L"VxInfiniteTeesBridge") || GetFileAttributesW(file)!=INVALID_FILE_ATTRIBUTES ||
		GetLastError()!=ERROR_FILE_NOT_FOUND || !RemoveDirectoryW(parent)) return 13;
	/* Regular-file destination also refuses; post-close rename positive control. */
	if(!CreateDirectoryW(parent,NULL) || !private_context(&c,base) || !fixture_file(c.root)) return 14;
	if(setup_pin_root(&c,&created,&slot) || !vx_close(&c) || !DeleteFileW(c.root)) return 15;
	if(!CreateDirectoryW(c.root,NULL)) return 19;
	write_directory=CreateFileW(c.root,GENERIC_WRITE,FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE,
		NULL,OPEN_EXISTING,FILE_FLAG_BACKUP_SEMANTICS,NULL);
	if(write_directory==INVALID_HANDLE_VALUE || !CloseHandle(write_directory)) return 20;
	if(!MoveFileW(c.root,renamed) || !RemoveDirectoryW(renamed) ||
		!RemoveDirectoryW(parent) || !RemoveDirectoryW(target) || !RemoveDirectoryW(base)) return 16;
	puts("private root pin/reparse/missing-file/delete/rename controls passed");
	return 0;
}
