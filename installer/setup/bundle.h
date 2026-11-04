/* SPDX-License-Identifier: MIT */
#ifndef VX_BUNDLE_H
#define VX_BUNDLE_H
#include <stdint.h>
#include <string.h>

#define VX_BUNDLE_COUNT 10
#define VX_BUNDLE_LIMIT (64U * 1024U * 1024U)
#define VX_BUNDLE_FILE_LIMIT (32U * 1024U * 1024U)
#pragma pack(push, 1)
typedef struct { uint32_t size; unsigned char hash[32]; } VX_BUNDLE_ENTRY;
typedef struct {
	char magic[8];
	uint64_t stub_size;
	char revision[40];
	unsigned char stub_hash[32];
	VX_BUNDLE_ENTRY entries[VX_BUNDLE_COUNT];
} VX_BUNDLE;
#pragma pack(pop)
_Static_assert(sizeof(VX_BUNDLE)==448,"bundle wire layout");

static int bundle_layout(const VX_BUNDLE *bundle,uint64_t size,const char *revision)
{
	if(size>VX_BUNDLE_LIMIT || size<sizeof(*bundle) ||
		memcmp(bundle->magic,"VXBDL001",8) || memcmp(bundle->revision,revision,40) ||
		!bundle->stub_size || bundle->stub_size>2U*1024U*1024U) return 0;
	uint64_t total=bundle->stub_size+sizeof(*bundle);
	for(unsigned int i=0;i<VX_BUNDLE_COUNT;++i) {
		if(!bundle->entries[i].size || bundle->entries[i].size>VX_BUNDLE_FILE_LIMIT) return 0;
		total+=bundle->entries[i].size;
	}
	return total==size;
}
#endif
