/* SPDX-License-Identifier: MIT */
/* Private policy/kernel test only; never calls an MSI action or live context. */
#include "remove.c"
#include <objbase.h>

int wmain(void)
{
	struct { DWORD directory; const WCHAR *name; BOOL want; } cases[]={
		{REMOVE_ROOT,L"GSPconnect.exe",TRUE}, {REMOVE_ROOT,L"foreign.exe",FALSE},
		{REMOVE_STATE,L"{12345678-1234-1234-1234-123456789ABC}.msi",TRUE},
		{REMOVE_STATE,L"{12345678-1234-1234-1234-123456789abc}.msi",FALSE},
		{REMOVE_STATE,L"setup.exe",TRUE}, {REMOVE_STATE,L"backup.msi",FALSE},
		{REMOVE_LOGS,L"bridge.0.jsonl",TRUE}, {REMOVE_LOGS,L"bridge.4.jsonl",TRUE},
		{REMOVE_LOGS,L"bridge.5.jsonl",FALSE}, {REMOVE_LOGS,L"export.jsonl",FALSE},
		{REMOVE_CONFIG,L"destination.json",TRUE}, {REMOVE_CONFIG,L"destination.backup.json",FALSE},
		{REMOVE_CONFIG,L".destination-0.tmp",TRUE},
		{REMOVE_CONFIG,L".destination-4294967295.tmp",TRUE},
		{REMOVE_CONFIG,L".destination-4294967296.tmp",FALSE},
		{REMOVE_CONFIG,L".destination-.tmp",FALSE}, {REMOVE_CONFIG,L".destination-01.tmp",FALSE},
		{REMOVE_CONFIG,L".destination-12345678901.tmp",FALSE}, {REMOVE_CONFIG,L".destination-foreign.tmp",FALSE},
		{REMOVE_SHORTCUTS,L"VX Bridge.lnk",TRUE}, {REMOVE_SHORTCUTS,L"VX Bridge Setup.lnk",TRUE},
		{REMOVE_SHORTCUTS,L"VX Bridge Diagnostics.lnk",TRUE}, {REMOVE_SHORTCUTS,L"Other.lnk",FALSE}
	};
	for(unsigned int i=0;i<ARRAYSIZE(cases);++i)
		if(known_file(cases[i].directory,cases[i].name)!=cases[i].want) return (int)i+1;
	if(!destination_temp(L".destination-1.tmp") || cache_name(L"*.msi")) return 30;
	REMOVE_PLAN plan={0};
	for(unsigned int i=0;i<REMOVE_MAX_FILES;++i) {
		WCHAR temporary[96];
		if(swprintf(temporary,96,L".destination-%u.tmp",i)<=0 || !add_file(&plan,REMOVE_CONFIG,temporary)) return 36;
	}
	if(plan.count!=REMOVE_MAX_FILES || add_file(&plan,REMOVE_CONFIG,L"destination.json") ||
		!add_file(&plan,REMOVE_CONFIG,L".destination-0.tmp")) return 37;
	VX_CONTEXT c={0}; WCHAR temp[MAX_PATH],id[39],name[96],root[MAX_PATH],path[MAX_PATH]; GUID guid;
	if(!GetTempPathW(MAX_PATH,temp) || CoCreateGuid(&guid)!=S_OK || StringFromGUID2(&guid,id,39)!=39 ||
		swprintf(name,96,L"VX-Private-Remove-%ls",id)<=0) return 31;
	size_t n=wcslen(temp); if(n && temp[n-1]==L'\\') temp[n-1]=0;
	if(!vx_path(root,temp,name) || !CreateDirectoryW(root,NULL) ||
		!vx_path(path,root,L"nonempty.txt")) return 32;
	HANDLE file=vx_file(path,FALSE,GENERIC_READ|GENERIC_WRITE|DELETE,FILE_SHARE_READ,CREATE_NEW);
	if(file==INVALID_HANDLE_VALUE || remove_directory(&c,root)) return 33;
	FILE_DISPOSITION_INFO disposition={TRUE};
	if(!SetFileInformationByHandle(file,FileDispositionInfo,&disposition,sizeof(disposition)) ||
		!CloseHandle(file)) return 34;
	if(!remove_directory(&c,root) || !remove_directory(&c,root) || !vx_close(&c)) return 35;
	return 0;
}
