/* Host-only deterministic native provider and factory codec contract mock. */
#include "../src/factory_adapter.h"
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
static unsigned init_n,create_n,sync_n,reg_n,push_n,process_n,unreg_n,destroy_n,deinit_n;
static uint8_t pipeline[0x140] __attribute__((aligned(16)));
static uintptr_t provider_vtable[8],provider[1];
static rook_writer32 *active_writer;
static uint8_t raw_snapshot[2304];
static int32_t planar[768];
static int16_t stereo[256];
static int32_t last_sec,last_nsec;
static int phase;
static int fail(const char *s) {
 const char *v=getenv("ASP1_TEST_FAULT");return v && !strcmp(v,s);
}
static int32_t mock_init(const asp_init32 *ip) {
 ++init_n;puts("mock vendor stdout chatter, must not appear in wire");
 const char *resource=getenv("ASP1_TEST_RESOURCE");
 struct stat st;
 return !ip || ip->reserved || ip->register_binder_service ||
   !resource || stat(resource,&st) || !S_ISREG(st.st_mode) || fail("init") ? -1:0;
}
static void *mock_create(uint32_t t,uint32_t fmt,uint32_t ch,uint32_t rate,
                         uint32_t ofmt,uint32_t och,uint32_t orate) {
 ++create_n;
 if(t || fmt!=6 || ch!=6 || rate!=16000 || ofmt!=1 || och!=1 ||
    orate!=16000 || fail("create"))return NULL;
 memcpy(pipeline+0x138,&(void*){provider},sizeof(void*));return pipeline;
}
static int32_t mock_sync(void *p,int32_t s) {
 ++sync_n;return p!=pipeline || s!=2 || fail("ref") ? -1:0;
}
static void mock_unpack(uint32_t d[8],const void *p,uint32_t fmt) {
 if(fmt!=6 || d[0] || d[1]!=6 || d[2]!=128 || d[3] ||
    d[4]!=128 || d[5] || d[6] || !d[7]){phase=-1;return;}
 memcpy(raw_snapshot,p,sizeof(raw_snapshot));
 for(unsigned frame=0;frame<128;frame++)
  for(unsigned ch=0;ch<6;ch++) {
   const uint8_t *b=raw_snapshot+frame*18u+ch*3u;
   uint32_t u=(uint32_t)b[0] | (uint32_t)b[1]<<8 | (uint32_t)b[2]<<16;
   if(u & 0x800000)u|=0xff000000;
   planar[ch*128u+frame]=(int32_t)u;
  }
}
static void mock_pack(uint32_t d[8],void *p,uint32_t fmt) {
 if(fmt!=1 || d[0] || d[1]!=2 || d[2]!=128 || d[3] ||
    d[4]!=128 || d[5] || d[6] || !d[7]){phase=-1;return;}
 int16_t *out=p;
 for(unsigned frame=0;frame<128;frame++)
  for(unsigned ch=0;ch<2;ch++) {
   int32_t v=planar[(ch+4u)*128u+frame];
   out[frame*2u+ch]=(int16_t)(v>=0?v/256:-((-(int64_t)v+255)/256));
  }
 memcpy(stereo,out,sizeof(stereo));
}
static int32_t mock_reg(void *p,rook_writer32 *w) {
 ++reg_n;
 if(p!=provider || !w || w->provider!=provider || w->pcm_format!=1 ||
    w->channels!=2 || w->sample_rate!=16000 || w->source_mode!=4 ||
    active_writer || fail("register"))return -1;
 active_writer=w;return 0;
}
static int32_t mock_push(void *p,rook_writer32 *w,const int16_t *s,
    uint32_t frames,int32_t flags,int32_t sec,int32_t nsec,const void *sidecar) {
 ++push_n;
 if(p!=provider || w!=active_writer || frames!=128 || flags || sidecar ||
    sec!=(int32_t)((push_n-1)/125u) ||
    nsec!=(int32_t)(((push_n-1)%125u)*8000000u) ||
    memcmp(s,stereo,sizeof(stereo)) || phase<0 || fail("push"))return -1;
 last_sec=sec;last_nsec=nsec;phase=1;return 0;
}
static int32_t mock_process(void *p,const void *raw,uint32_t *ni,void *out,
    uint32_t *no,int32_t context,asp_time32 stamp) {
 ++process_n;
 if(p!=pipeline || !ni || !no || *ni!=128 || *no!=128 || context ||
    stamp.sec!=last_sec || stamp.nsec!=last_nsec || phase!=1 ||
    memcmp(raw,raw_snapshot,sizeof(raw_snapshot)) || fail("process"))return -1;
 phase=0;
 int16_t *dst=out;
 int16_t value=(int16_t)(101u+((process_n-1u)%1000u));
 for(unsigned i=0;i<128;i++)dst[i]=value;
 if(fail("counts"))--*no;
 if(fail("overrun"))((uint8_t*)out)[256]=0;
 if(fail("mutate"))((uint8_t*)(uintptr_t)raw)[0]^=1;
 return 0;
}
static int32_t mock_unreg(void *p,rook_writer32 *w) {
 ++unreg_n;if(p!=provider || w!=active_writer)return -1;
 active_writer=NULL;return fail("unregister")?-1:0;
}
static int32_t mock_destroy(void *p) {++destroy_n;return p!=pipeline || fail("destroy");}
static void mock_deinit(void) {++deinit_n;}
int asp_vendor_load(factory_abi *out) {
 memset(provider_vtable,0,sizeof(provider_vtable));
 /* Match the production vtable ownership check without casting function pointers. */
 int32_t (*register_fn)(void*,rook_writer32*)=mock_reg;
 int32_t (*unregister_fn)(void*,rook_writer32*)=mock_unreg;
 int32_t (*push_fn)(void*,rook_writer32*,const int16_t*,uint32_t,int32_t,int32_t,int32_t,const void*)=mock_push;
 memcpy(&provider_vtable[4],&register_fn,sizeof(register_fn));
 memcpy(&provider_vtable[5],&unregister_fn,sizeof(unregister_fn));
 memcpy(&provider_vtable[7],&push_fn,sizeof(push_fn));
 provider[0]=(uintptr_t)provider_vtable;
 *out=(factory_abi){0};
 out->init=mock_init;out->create=mock_create;out->ref_sync=mock_sync;
 out->process=mock_process;out->destroy=mock_destroy;out->deinit=mock_deinit;
 out->unpack=mock_unpack;out->pack=mock_pack;
 out->register_writer=mock_reg;out->unregister_writer=mock_unreg;out->push=mock_push;
 out->expected_vtable=(uintptr_t)provider_vtable;return 0;
}
void asp_vendor_unload(void) {
 fprintf(stderr,"mock counts init=%u create=%u ref=%u register=%u push=%u process=%u unregister=%u destroy=%u deinit=%u\n",
 init_n,create_n,sync_n,reg_n,push_n,process_n,unreg_n,destroy_n,deinit_n);
}
