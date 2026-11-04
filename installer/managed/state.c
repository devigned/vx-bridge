/* SPDX-License-Identifier: MIT */
#define COBJMACROS
#include "state.h"
#include <shlobj.h>
#include <sddl.h>
#include <bcrypt.h>
#include <wchar.h>
#include <string.h>

const WCHAR *vx_files[VX_COUNT] = {
	L"GSPconnect.exe", L"vxbridge.exe", L"LICENSE", L"THIRD_PARTY_NOTICES.txt",
	L"build-manifest.json", L"SHA256SUMS"
};
BOOL vx_path(WCHAR *out, const WCHAR *directory, const WCHAR *name)
{
	size_t n = wcslen(directory), m = wcslen(name);
	if (n + m + 2 > MAX_PATH) return FALSE;
	memcpy(out, directory, n * sizeof(WCHAR));
	out[n] = L'\\';
	memcpy(out + n + 1, name, (m + 1) * sizeof(WCHAR));
	return TRUE;
}
BOOL vx_ascii(char *out, unsigned int width, const WCHAR *text)
{
	size_t n = wcslen(text);
	if (n >= width) return FALSE;
	memset(out, 0, width);
	for (size_t i = 0; i < n; ++i) {
		if (text[i] < 0x20 || text[i] > 0x7e) return FALSE;
		out[i] = (char)text[i];
	}
	return TRUE;
}
BOOL vx_wide(WCHAR *out, unsigned int width, const char *text)
{
	size_t n = strlen(text);
	if (n >= width) return FALSE;
	for (size_t i = 0; i <= n; ++i) out[i] = (WCHAR)(unsigned char)text[i];
	return TRUE;
}
BOOL vx_hex(const char *text, unsigned int count)
{
	for (unsigned int i = 0; i < count; ++i)
		if (!((text[i] >= '0' && text[i] <= '9') || (text[i] >= 'a' && text[i] <= 'f'))) return FALSE;
	return text[count] == '\0';
}
BOOL vx_guid(const char *text)
{
	if (text[0] != '{' || text[37] != '}' || text[38]) return FALSE;
	if(!strcmp(text,"{00000000-0000-0000-0000-000000000000}")) return FALSE;
	for (unsigned int i = 1; i < 37; ++i) {
		if (i == 9 || i == 14 || i == 19 || i == 24) { if (text[i] != '-') return FALSE; }
		else if (!((text[i] >= '0' && text[i] <= '9') || (text[i] >= 'A' && text[i] <= 'F'))) return FALSE;
	}
	return TRUE;
}
static BOOL padded(const char *s, unsigned int width)
{
	BOOL zero = FALSE;
	for (unsigned int i = 0; i < width; ++i) {
		if (!s[i]) zero = TRUE;
		else if (zero || s[i] < 0x20 || s[i] > 0x7e) return FALSE;
	}
	return zero;
}
BOOL vx_receipt(const VX_RECEIPT *r)
{
	if (memcmp(r->magic, "VXMSI01", 8) || r->size != sizeof(*r) || r->schema != 1 ||
		!padded(r->product, 39) || !padded(r->package, 39) ||
		!padded(r->version, 32) || !padded(r->revision, 41) ||
		!vx_guid(r->product) || !vx_guid(r->package) || !strcmp(r->product, r->package) ||
		!vx_hex(r->revision, 40)) return FALSE;
	unsigned int fields = 0, value = 0, digits = 0;
	for (const char *p = r->version; ; ++p) {
		if (*p >= '0' && *p <= '9') {
			if (digits && value == 0) return FALSE;
			value = value * 10 + (unsigned int)(*p - '0'); digits++;
			if (digits > 5 || value > (fields == 2 ? 65535U : 255U)) return FALSE;
		} else {
			if (!digits || (*p != '.' && *p != 0)) return FALSE;
			if (!*p) { if (fields != 2) return FALSE; break; }
			if (++fields > 2) return FALSE;
			value = digits = 0;
		}
	}
	for (unsigned int i = 0; i < VX_COUNT; ++i)
		if (!padded(r->hashes[i], 65) || !vx_hex(r->hashes[i], 64)) return FALSE;
	for (unsigned int i = 0; i < sizeof(r->reserved); ++i) if (r->reserved[i]) return FALSE;
	return TRUE;
}
BOOL vx_pending(const VX_PENDING *p)
{
	return !memcmp(p->magic, "VXTX001", 8) && p->size == sizeof(*p) &&
		p->operation >= VX_INSTALL && p->operation <= VX_REPAIR &&
		p->phase >= VX_WAITING && p->phase <= VX_COMMITTED && p->has_before <= 1 &&
		!p->reserved && vx_guid(p->nonce) && vx_receipt(&p->after) &&
		(!p->has_before || vx_receipt(&p->before)) &&
		p->run_saved <= 1 && p->run_changed <= 1 && p->run_command[260] == 0;
}
HANDLE vx_file(const WCHAR *path, BOOL directory, DWORD access, DWORD share, DWORD creation)
{
	HANDLE h = CreateFileW(path, access, share, NULL, creation,
		FILE_FLAG_OPEN_REPARSE_POINT | (directory ? FILE_FLAG_BACKUP_SEMANTICS : 0), NULL);
	BY_HANDLE_FILE_INFORMATION info;
	WCHAR final[MAX_PATH + 8];
	if (h == INVALID_HANDLE_VALUE) return h;
	DWORD n = GetFinalPathNameByHandleW(h, final, ARRAYSIZE(final), 0);
	if (!GetFileInformationByHandle(h, &info) || !n || n >= ARRAYSIZE(final) ||
		info.dwFileAttributes & FILE_ATTRIBUTE_REPARSE_POINT ||
		!!(info.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY) != !!directory ||
		(!directory && info.nNumberOfLinks != 1) || wcsncmp(final, L"\\\\?\\", 4) ||
		_wcsicmp(final + 4, path)) {
		CloseHandle(h); SetLastError(ERROR_INVALID_DATA); return INVALID_HANDLE_VALUE;
	}
	return h;
}
BOOL vx_pin_tree(VX_CONTEXT *c, const WCHAR *path)
{
	WCHAR current[MAX_PATH];
	if (wcslen(path) >= MAX_PATH || wcslen(path) < 4 || path[1] != L':' || path[2] != L'\\') return FALSE;
	wcscpy(current, path);
	for (WCHAR *p = current + 3; ; ++p) {
		if (*p && *p != L'\\') continue;
		WCHAR old = *p; *p = 0;
		if (c->count >= ARRAYSIZE(c->pins)) return FALSE;
		HANDLE h = vx_file(current, TRUE, FILE_READ_ATTRIBUTES, FILE_SHARE_READ | FILE_SHARE_WRITE, OPEN_EXISTING);
		*p = old;
		if (h == INVALID_HANDLE_VALUE) return FALSE;
		c->pins[c->count++] = h;
		if (!old) break;
	}
	return TRUE;
}
BOOL vx_context(VX_CONTEXT *c)
{
	HANDLE token = NULL;
	DWORD size = 0;
	TOKEN_USER *user = NULL;
	WCHAR *sid = NULL, *base = NULL;
	PSECURITY_DESCRIPTOR sd = NULL;
	WCHAR text[640], app[MAX_PATH];
	BOOL ok = FALSE;
	memset(c, 0, sizeof(*c));
	DWORD token_access=TOKEN_QUERY|TOKEN_IMPERSONATE|TOKEN_DUPLICATE;
	if (!OpenThreadToken(GetCurrentThread(), token_access, TRUE, &token)) {
		if (GetLastError() != ERROR_NO_TOKEN || !OpenProcessToken(GetCurrentProcess(), token_access, &token)) goto done;
	}
	if (GetTokenInformation(token, TokenUser, NULL, 0, &size) || GetLastError() != ERROR_INSUFFICIENT_BUFFER || size > 65536) goto done;
	user = HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, size);
	if (!user || !GetTokenInformation(token, TokenUser, user, size, &size) ||
		!ConvertSidToStringSidW(user->User.Sid, &sid) || wcslen(sid) >= ARRAYSIZE(c->sid) ||
		!wcscmp(sid, L"S-1-5-18") || SHGetKnownFolderPath(&FOLDERID_LocalAppData, KF_FLAG_DONT_VERIFY, token, &base) != S_OK ||
		wcslen(base) >= MAX_PATH) goto done;
	wcscpy(c->sid, sid); wcscpy(c->base, base);
	if (!vx_path(app, base, L"Programs") || !vx_path(c->root, app, L"VxInfiniteTeesBridge") ||
		!vx_path(app, base, L"VxInfiniteTeesBridge") || !vx_path(c->state, app, L"installer")) goto done;
	if (!vx_pin_tree(c,base)) goto done;
	if (swprintf(text, ARRAYSIZE(text), L"O:%lsD:P(A;;GA;;;SY)(A;;GA;;;%ls)", sid, sid) <= 0 ||
		!ConvertStringSecurityDescriptorToSecurityDescriptorW(text, SDDL_REVISION_1, &sd, NULL)) goto done;
	SECURITY_ATTRIBUTES sa = { sizeof(sa), sd, FALSE };
	if (swprintf(text, ARRAYSIZE(text), L"Global\\VxBridge.ManagedInstall.%ls", sid) <= 0) goto done;
	c->gate = CreateMutexW(&sa, FALSE, text);
	if (!c->gate) goto done;
	c->user_token = token; token = NULL;
	ok = TRUE;
done:
	if (sd) LocalFree(sd);
	if (base) CoTaskMemFree(base);
	if (sid) LocalFree(sid);
	if (user) HeapFree(GetProcessHeap(), 0, user);
	if (token && !CloseHandle(token)) ok=FALSE;
	if (!ok) vx_close(c);
	return ok;
}
BOOL vx_open_state(VX_CONTEXT *c, BOOL create)
{
	HKEY hkcu=NULL;
	PSECURITY_DESCRIPTOR sd=NULL;
	WCHAR text[640], app[MAX_PATH];
	BOOL ok=FALSE;
	if(c->key) return TRUE;
	if(RegOpenCurrentUser(KEY_READ|KEY_WRITE,&hkcu)!=ERROR_SUCCESS) goto done;
	if(create) {
		if(swprintf(text,ARRAYSIZE(text),L"O:%lsD:P(A;;GA;;;SY)(A;;GA;;;%ls)",c->sid,c->sid)<=0 ||
			!ConvertStringSecurityDescriptorToSecurityDescriptorW(text,SDDL_REVISION_1,&sd,NULL) ||
			!vx_path(app,c->base,L"VxInfiniteTeesBridge")) goto done;
		SECURITY_ATTRIBUTES sa={sizeof(sa),sd,FALSE};
		if(!CreateDirectoryW(app,&sa) && GetLastError()!=ERROR_ALREADY_EXISTS) goto done;
		if(!vx_pin_tree(c,app)) goto done;
		if(!CreateDirectoryW(c->state,&sa) && GetLastError()!=ERROR_ALREADY_EXISTS) goto done;
		if(!vx_pin_tree(c,c->state) ||
			RegCreateKeyExW(hkcu,VX_STATE_KEY,0,NULL,0,KEY_READ|KEY_WRITE,&sa,&c->key,NULL)!=ERROR_SUCCESS) goto done;
	} else {
		LSTATUS status=RegOpenKeyExW(hkcu,VX_STATE_KEY,0,KEY_READ|KEY_WRITE,&c->key);
		if(status==ERROR_FILE_NOT_FOUND) { c->key=NULL; ok=TRUE; goto done; }
		if(status!=ERROR_SUCCESS || !vx_pin_tree(c,c->state)) goto done;
	}
	ok=TRUE;
done:
	if(hkcu && RegCloseKey(hkcu)!=ERROR_SUCCESS) ok=FALSE;
	if(sd) LocalFree(sd);
	return ok;
}
BOOL vx_enter(VX_CONTEXT *c) { DWORD w = WaitForSingleObject(c->gate, 500); return w == WAIT_OBJECT_0 || w == WAIT_ABANDONED; }
BOOL vx_leave(VX_CONTEXT *c) { return ReleaseMutex(c->gate); }
BOOL vx_close(VX_CONTEXT *c)
{
	BOOL ok = TRUE;
	if (c->key && RegCloseKey(c->key) != ERROR_SUCCESS) ok = FALSE;
	if (c->gate && !CloseHandle(c->gate)) ok = FALSE;
	if (c->user_token && !CloseHandle(c->user_token)) ok = FALSE;
	while (c->count) if (!CloseHandle(c->pins[--c->count])) ok = FALSE;
	c->key = NULL; c->gate = NULL; c->user_token = NULL;
	return ok;
}
BOOL vx_binary(VX_CONTEXT *c, const WCHAR *name, void *data, DWORD size, BOOL *exists)
{
	if(!c->key) { *exists=FALSE; return TRUE; }
	DWORD type = 0, actual = size;
	LSTATUS status = RegQueryValueExW(c->key, name, NULL, &type, data, &actual);
	if (status == ERROR_FILE_NOT_FOUND) { *exists = FALSE; return TRUE; }
	*exists = TRUE;
	return status == ERROR_SUCCESS && type == REG_BINARY && actual == size;
}
BOOL vx_put(VX_CONTEXT *c, const WCHAR *name, const void *data, DWORD size)
{
	return RegSetValueExW(c->key, name, 0, REG_BINARY, data, size) == ERROR_SUCCESS && RegFlushKey(c->key) == ERROR_SUCCESS;
}
BOOL vx_delete(VX_CONTEXT *c, const WCHAR *name)
{
	return RegDeleteValueW(c->key, name) == ERROR_SUCCESS && RegFlushKey(c->key) == ERROR_SUCCESS;
}
BOOL vx_hash(HANDLE file, char out[65])
{
	BCRYPT_ALG_HANDLE alg = NULL;
	BCRYPT_HASH_HANDLE hash = NULL;
	BYTE digest[32], buffer[8192];
	DWORD n = 0;
	ULONGLONG total = 0;
	BOOL ok = FALSE;
	LARGE_INTEGER zero = { 0 };
	if (!SetFilePointerEx(file, zero, NULL, FILE_BEGIN) ||
		BCryptOpenAlgorithmProvider(&alg, BCRYPT_SHA256_ALGORITHM, NULL, 0) < 0 ||
		BCryptCreateHash(alg, &hash, NULL, 0, NULL, 0, 0) < 0) goto done;
	for (;;) {
		if (!ReadFile(file, buffer, sizeof(buffer), &n, NULL)) goto done;
		if (!n) break;
		total += n;
		if (total > 32U * 1024U * 1024U || BCryptHashData(hash, buffer, n, 0) < 0) goto done;
	}
	if (BCryptFinishHash(hash, digest, sizeof(digest), 0) < 0) goto done;
	for (unsigned int i = 0; i < 32; ++i) {
		static const char hex[] = "0123456789abcdef";
		out[2*i] = hex[digest[i] >> 4]; out[2*i+1] = hex[digest[i] & 15];
	}
	out[64] = 0; ok = TRUE;
done:
	if (hash && BCryptDestroyHash(hash) < 0) ok = FALSE;
	if (alg && BCryptCloseAlgorithmProvider(alg, 0) < 0) ok = FALSE;
	return ok;
}
BOOL vx_payload(VX_CONTEXT *c, const VX_RECEIPT *r, BOOL empty)
{
	WCHAR path[MAX_PATH];
	WIN32_FIND_DATAW entry;
	wcscpy(path,c->root);
	for (;;) {
		DWORD attributes=GetFileAttributesW(path);
		if(attributes!=INVALID_FILE_ATTRIBUTES) {
			if(!(attributes&FILE_ATTRIBUTE_DIRECTORY) || !vx_pin_tree(c,path)) return FALSE;
			break;
		}
		DWORD e=GetLastError();
		if(!empty || (e!=ERROR_FILE_NOT_FOUND && e!=ERROR_PATH_NOT_FOUND)) return FALSE;
		WCHAR *slash=wcsrchr(path,L'\\');
		if(!slash || slash<=path+2) return FALSE;
		*slash=0;
	}
	if (!vx_path(path, c->root, L"*")) return FALSE;
	HANDLE find = FindFirstFileW(path, &entry);
	if (find == INVALID_HANDLE_VALUE) {
		DWORD e = GetLastError();
		return empty && (e == ERROR_FILE_NOT_FOUND || e == ERROR_PATH_NOT_FOUND);
	}
	unsigned int count = 0; BOOL ok = TRUE;
	do {
		if (!wcscmp(entry.cFileName, L".") || !wcscmp(entry.cFileName, L"..")) continue;
		if (empty || entry.dwFileAttributes & (FILE_ATTRIBUTE_DIRECTORY | FILE_ATTRIBUTE_REPARSE_POINT)) { ok = FALSE; break; }
		unsigned int i;
		for (i = 0; i < VX_COUNT; ++i) if (!wcscmp(entry.cFileName, vx_files[i])) break;
		if (i == VX_COUNT) { ok = FALSE; break; }
		count++;
	} while (FindNextFileW(find, &entry));
	if (ok && GetLastError() != ERROR_NO_MORE_FILES) ok = FALSE;
	if (!FindClose(find)) ok = FALSE;
	if (!ok || (empty ? count != 0 : count != VX_COUNT)) return FALSE;
	if (empty) return TRUE;
	for (unsigned int i = 0; i < VX_COUNT; ++i) {
		char hash[65];
		if (!vx_path(path, c->root, vx_files[i])) return FALSE;
		HANDLE h = vx_file(path, FALSE, GENERIC_READ, FILE_SHARE_READ, OPEN_EXISTING);
		if (h == INVALID_HANDLE_VALUE) return FALSE;
		ok = vx_hash(h, hash) && !strcmp(hash, r->hashes[i]);
		if (!CloseHandle(h)) ok = FALSE;
		if (!ok) return FALSE;
	}
	return TRUE;
}
BOOL vx_registered(VX_CONTEXT *c, const VX_RECEIPT *r, BOOL absent)
{
	WCHAR product[39], value[MAX_PATH], expected[64];
	if (!vx_wide(product, ARRAYSIZE(product), r->product)) return FALSE;
	DWORD n = ARRAYSIZE(value);
	UINT status = MsiGetProductInfoExW(product, c->sid, MSIINSTALLCONTEXT_USERUNMANAGED, INSTALLPROPERTY_PRODUCTSTATE, value, &n);
	if (absent) return status == ERROR_UNKNOWN_PRODUCT;
	if (status != ERROR_SUCCESS || wcscmp(value, L"5")) return FALSE;
	const WCHAR *properties[] = { INSTALLPROPERTY_ASSIGNMENTTYPE, INSTALLPROPERTY_PACKAGECODE, INSTALLPROPERTY_VERSIONSTRING, INSTALLPROPERTY_INSTALLLOCATION, INSTALLPROPERTY_TRANSFORMS };
	for (unsigned int i = 0; i < ARRAYSIZE(properties); ++i) {
		const WCHAR *want = expected;
		if (i == 0) wcscpy(expected, L"0");
		if (i == 1 && !vx_wide(expected, ARRAYSIZE(expected), r->package)) return FALSE;
		if (i == 2 && !vx_wide(expected, ARRAYSIZE(expected), r->version)) return FALSE;
		if (i == 3) want = c->root;
		if (i == 4) expected[0] = 0;
		n = ARRAYSIZE(value);
		if (MsiGetProductInfoExW(product, c->sid, MSIINSTALLCONTEXT_USERUNMANAGED, properties[i], value, &n) != ERROR_SUCCESS) return FALSE;
		if (i == 3 && n && value[n-1] == L'\\') value[n-1] = 0;
		if (_wcsicmp(value, want)) return FALSE;
	}
	return TRUE;
}
UINT vx_error(MSIHANDLE session, const WCHAR *reason)
{
	MSIHANDLE message = MsiCreateRecord(0);
	if (message) {
		if (MsiRecordSetStringW(message, 0, reason) == ERROR_SUCCESS) MsiProcessMessage(session, INSTALLMESSAGE_ERROR, message);
		MsiCloseHandle(message);
	}
	return ERROR_INSTALL_FAILURE;
}
