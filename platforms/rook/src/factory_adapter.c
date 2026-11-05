/* Factory codec on REF, native RefProvider push, then raw public PipelineAsr. */
#include "factory_adapter.h"
#ifdef ASP_BIONIC
#include "bionic_compat.h"
#else
#include <stdint.h>
#include <limits.h>
#include <string.h>
#endif
#ifdef ASP_BIONIC
_Static_assert(sizeof(asp_init32)==8,"init ABI");
_Static_assert(sizeof(rook_writer32)==20,"writer ABI");
_Static_assert(__builtin_offsetof(rook_writer32,source_mode)==16,"writer offset");
#endif
static int intact(const uint8_t *p,size_t n) {
 for(size_t i=0;i<n;i++) if(p[i]!=0xa5) return 0;
 return 1;
}
int factory_close(factory_adapter *a) {
 if(!a || a->closed) return 0;
 a->closed=1;int rc=0;
 if(a->registered) {
  a->registered=0;
  if(a->abi.unregister_writer(a->provider,&a->writer)) rc=-1;
 }
 if(a->pipeline) {
  void *p=a->pipeline;a->pipeline=NULL;
  if(a->abi.destroy(p)) rc=-1;
 }
 if(a->initialized) {a->initialized=0;a->abi.deinit();}
 return rc;
}
int factory_open(factory_adapter *a,const factory_abi *v) {
 if(!a) return -1;
 memset(a,0,sizeof(*a));
 if(!v || !v->init || !v->create || !v->ref_sync || !v->process ||
    !v->destroy || !v->deinit || !v->unpack || !v->pack ||
    !v->register_writer || !v->unregister_writer || !v->push ||
    !v->expected_vtable) {a->terminal=1;return -1;}
 a->abi=*v;
 const asp_init32 init={0,0};
 if(a->abi.init(&init)) {a->terminal=1;return -1;}
 a->initialized=1;
 a->pipeline=a->abi.create(0,6,6,16000,1,1,16000);
 if(!a->pipeline || a->abi.ref_sync(a->pipeline,2)) goto failed;
 a->provider=*(void**)((uint8_t*)a->pipeline+0x138);
 if(!a->provider || (uintptr_t)a->provider%sizeof(void*)) goto failed;
 if(*(uintptr_t*)a->provider!=a->abi.expected_vtable) goto failed;
 /* Verify that virtual writer methods match the pinned provider ABI. */
 const uintptr_t *vt=(const uintptr_t*)a->abi.expected_vtable;
 uintptr_t reg,unreg,push;
 memcpy(&reg,&a->abi.register_writer,sizeof(reg));
 memcpy(&unreg,&a->abi.unregister_writer,sizeof(unreg));
 memcpy(&push,&a->abi.push,sizeof(push));
 if(vt[4]!=reg || vt[5]!=unreg || vt[7]!=push) goto failed;
 a->writer=(rook_writer32){a->provider,1,2,16000,4};
 if(a->abi.register_writer(a->provider,&a->writer)) goto failed;
 a->registered=1;return 0;
failed:
 a->terminal=1;(void)factory_close(a);return -1;
}
int factory_process(factory_adapter *a,const uint8_t raw[2304],int16_t out[128]) {
 struct {uint8_t head[32];uint8_t data[2304] __attribute__((aligned(16)));uint8_t tail[32];} src;
 struct {uint8_t head[32];int16_t data[128] __attribute__((aligned(16)));uint8_t tail[32];} dst;
 if(!a || !raw || !out || a->terminal || a->closed || !a->registered) return -1;
 uint64_t sec=a->quantum_index/125u;
 if(sec>(uint64_t)INT32_MAX) {a->terminal=1;return -1;}
 asp_time32 ts={(int32_t)sec,(int32_t)((a->quantum_index%125u)*UINT64_C(8000000))};
 memset(&src,0xa5,sizeof(src));memcpy(src.data,raw,sizeof(src.data));
 memset(&dst,0xa5,sizeof(dst));memset(dst.data,0,sizeof(dst.data));
 memset(&a->planar,0xa5,sizeof(a->planar));
 memset(&a->reference,0xa5,sizeof(a->reference));
 uint32_t unpack[8]={0,6,128,0,128,0,0,(uint32_t)(uintptr_t)a->planar.data};
 uint32_t pack[8]={0,2,128,0,128,0,0,(uint32_t)(uintptr_t)(a->planar.data+4*128)};
 a->abi.unpack(unpack,src.data,6);
 a->abi.pack(pack,a->reference.data,1);
 if(!intact(a->planar.head,sizeof(a->planar.head)) ||
    !intact(a->planar.tail,sizeof(a->planar.tail)) ||
    !intact(a->reference.head,sizeof(a->reference.head)) ||
    !intact(a->reference.tail,sizeof(a->reference.tail)) ||
    !intact(src.head,sizeof(src.head)) || !intact(src.tail,sizeof(src.tail)) ||
    memcmp(src.data,raw,sizeof(src.data))) goto failed;
 if(a->abi.push(a->provider,&a->writer,a->reference.data,128,0,ts.sec,ts.nsec,NULL)) goto failed;
 uint32_t ni=128,no=128;
 int rc=a->abi.process(a->pipeline,src.data,&ni,dst.data,&no,0,ts);
 if(rc || ni!=128 || no!=128 ||
    !intact(src.head,sizeof(src.head)) || !intact(src.tail,sizeof(src.tail)) ||
    !intact(dst.head,sizeof(dst.head)) || !intact(dst.tail,sizeof(dst.tail)) ||
    memcmp(src.data,raw,sizeof(src.data))) goto failed;
 memcpy(out,dst.data,sizeof(dst.data));++a->quantum_index;return 0;
failed:
 a->terminal=1;return -1;
}
