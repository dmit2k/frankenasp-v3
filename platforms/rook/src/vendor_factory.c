/* Source-selected factory ABI: SHA-pinned ARM32/Bionic libasp, no mock fallback. */
#include "factory_adapter.h"
#include "sha256.h"
#ifdef ASP_BIONIC
#include "bionic_compat.h"
#else
#error "Rook factory binding requires ARM32 Bionic"
#endif
_Static_assert(sizeof(void*)==4,"Private ABI is ARM32 only");
_Static_assert(__BYTE_ORDER__==__ORDER_LITTLE_ENDIAN__,"Private ABI little endian");

#define FACTORY_PATH "/system/lib/libasp.so"
static void *library;
static void report(const char *s) { (void)write(2,s,strlen(s)); }
static const uint8_t pinned_sha[32]={
 0x32,0x58,0xe6,0x55,0x5b,0x46,0x0a,0x7b,
 0x6a,0x13,0xe4,0x70,0xa7,0xf6,0x8b,0x83,
 0x59,0xce,0xad,0xf6,0x6f,0xb4,0x93,0x0f,
 0x33,0x1c,0x89,0xd3,0xbd,0x33,0xfb,0x59
};
static int load_one(void *lib,const char *name,void *out,size_t width) {
 void *address=dlsym(lib,name);
 if(!address || width!=sizeof(address)) return -1;
 memcpy(out,&address,width);return 0;
}
static void copy_address(void *out,uintptr_t addr) {
 memcpy(out,&addr,sizeof(addr));
}
int asp_vendor_load(factory_abi *v) {
 if(!v || library) return -1;
 memset(v,0,sizeof(*v));
 uint8_t digest[32];
 if(file_sha256(FACTORY_PATH,digest) || memcmp(digest,pinned_sha,32)) {
  report("asp1: pinned libasp SHA verification failed\n");return -1;
 }
 library=dlopen(FACTORY_PATH,2);
 if(!library) {report("asp1: factory dlopen failed\n");return -1;}
 if(load_one(library,"asp_parameterized_init",&v->init,sizeof(v->init)) ||
    load_one(library,"asp_create_pipeline",&v->create,sizeof(v->create)) ||
    load_one(library,"asp_set_ext_ref_sync_status",&v->ref_sync,sizeof(v->ref_sync)) ||
    load_one(library,"asp_process",&v->process,sizeof(v->process)) ||
    load_one(library,"asp_destroy_pipeline",&v->destroy,sizeof(v->destroy)) ||
    load_one(library,"asp_deinit",&v->deinit,sizeof(v->deinit))) {
   report("asp1: missing factory API export\n");goto failed;
 }
 void *process_address;
 memcpy(&process_address,&v->process,sizeof(process_address));
 rook_dl_info info;
 if(!dladdr(process_address,&info) || !info.dli_fbase ||
    (!info.dli_fname || (strcmp(info.dli_fname,"libasp.so") &&
     strcmp(info.dli_fname,FACTORY_PATH)))) {
   report("asp1: factory dladdr mapping mismatch\n");goto failed;
 }
 uintptr_t base=(uintptr_t)info.dli_fbase;
 if((uintptr_t)process_address!=base+0x16addu) {
   report("asp1: factory code offset mismatch\n");goto failed;
 }
 /* VAs verified against the pinned ELF; code pointers are Thumb. */
 copy_address(&v->unpack,base+0x26e25u);
 copy_address(&v->pack,base+0x26ef9u);
 copy_address(&v->register_writer,base+0x170fdu);
 copy_address(&v->unregister_writer,base+0x173a1u);
 copy_address(&v->push,base+0x17a5du);
 v->expected_vtable=base+0x1aa448u;
 return 0;
failed:
 asp_vendor_unload();
 memset(v,0,sizeof(*v));return -1;
}
void asp_vendor_unload(void) {
 if(library){(void)dlclose(library);library=NULL;}
}
