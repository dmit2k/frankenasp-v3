#include "factory.h"
#include "bionic_compat.h"

#define FACTORY_PATH "/system/lib/libasp.so"
#define CONFIG_DIR "/vendor/etc/audio-algorithms/asp.cfg"
#define PIPE_ASR 0u
#define PIPE_SPEAKER 3u
#define FMT_PCM16 1u
#define FMT_PCM24_PACKED 6u
#define DEVICE_OUT_SPEAKER 2u

static void *library;

static void report(const char *s) { (void)write(2,s,strlen(s)); }

static int bind_one(const char *name,void *out) {
 void *address=dlsym(library,name);
 if(!address) {report("crown: missing export ");report(name);report("\n");return -1;}
 memcpy(out,&address,sizeof(address));
 return 0;
}

int asp_vendor_load(factory_abi *v) {
 if(!v || library) return -1;
 memset(v,0,sizeof(*v));
 library=dlopen(FACTORY_PATH,2);
 if(!library) {
  const char *e=dlerror();
  report("crown: dlopen libasp failed: ");report(e?e:"?");report("\n");
  return -1;
 }
 if(bind_one("asp_parameterized_init",&v->init) || bind_one("asp_create_pipeline",&v->create) ||
    bind_one("asp_process",&v->process) || bind_one("asp_destroy_pipeline",&v->destroy) ||
    bind_one("asp_deinit",&v->deinit) || bind_one("asp_set_device",&v->set_device) ||
    bind_one("asp_set_ext_ref_sync_status",&v->ref_sync)) {
  asp_vendor_unload();memset(v,0,sizeof(*v));return -1;
 }
 return 0;
}

void asp_vendor_unload(void) {
 if(library) {(void)dlclose(library);library=NULL;}
}

int factory_close(factory_adapter *a) {
 if(!a || a->closed) return 0;
 a->closed=1;
 int rc=0;
 if(a->asr) {void *p=a->asr;a->asr=NULL;if(a->abi.destroy(p)) rc=-1;}
 if(a->speaker) {void *p=a->speaker;a->speaker=NULL;if(a->abi.destroy(p)) rc=-1;}
 if(a->initialized) {a->initialized=0;a->abi.deinit();}
 return rc;
}

int factory_open(factory_adapter *a,const factory_abi *v) {
 if(!a) return -1;
 memset(a,0,sizeof(*a));
 if(!v || !v->init || !v->create || !v->process || !v->destroy || !v->deinit || !v->set_device || !v->ref_sync) {
  a->terminal=1;return -1;
 }
 a->abi=*v;
 static crown_init_info info;
 memset(&info,0,sizeof(info));
 strcpy(info.config_dir,CONFIG_DIR);
 int rc=a->abi.init(&info);
 if(rc) {report("crown: asp init failed\n");a->terminal=1;return -1;}
 a->initialized=1;
 a->use_speaker=getenv("CROWN_ASP_NO_SPEAKER_REF")==NULL;
 {
  const char *s=getenv("CROWN_ASP_LEAD");
  a->lead=s && s[0]>='0' && s[0]<='8' ? (unsigned)(s[0]-'0') : 2u;
  if(!a->use_speaker) a->lead=0;
 }
 if(a->use_speaker) {
  a->speaker=a->abi.create(PIPE_SPEAKER,FMT_PCM16,2,48000,FMT_PCM16,2,48000);
  if(!a->speaker) {report("crown: speaker pipeline failed\n");goto failed;}
  if(a->abi.set_device(a->speaker,DEVICE_OUT_SPEAKER)) report("crown: set_device failed\n");
 }
 a->asr=a->abi.create(PIPE_ASR,FMT_PCM24_PACKED,6,16000,FMT_PCM16,1,16000);
 if(!a->asr) {report("crown: asr pipeline failed\n");goto failed;}
 if(a->use_speaker) {
  const char *s=getenv("CROWN_ASP_REF_SYNC");
  int32_t status=s && s[0]>='0' && s[0]<='9' ? s[0]-'0' : 2;
  if(status && a->abi.ref_sync(a->asr,status)) report("crown: ref sync status failed\n");
 }
 return 0;
failed:
 a->terminal=1;(void)factory_close(a);return -1;
}

static int16_t ref_sample(const uint8_t *frame,unsigned offset) {
 return (int16_t)((uint16_t)frame[offset+1] | (uint16_t)((uint16_t)frame[offset+2]<<8));
}

static void upsample(factory_adapter *a,const uint8_t *raw) {
 for(unsigned n=0;n<CROWN_QUANTUM;n++) {
  const uint8_t *f=raw+(size_t)n*CROWN_IN_STRIDE;
  int32_t l=ref_sample(f,12),r=ref_sample(f,15);
  for(unsigned k=0;k<3;k++) {
   int32_t yl=a->last_l+((l-a->last_l)*(int32_t)(k+1))/3;
   int32_t yr=a->last_r+((r-a->last_r)*(int32_t)(k+1))/3;
   a->ref48[(n*3+k)*2]=(int16_t)yl;
   a->ref48[(n*3+k)*2+1]=(int16_t)yr;
  }
  a->last_l=(int16_t)l;a->last_r=(int16_t)r;
 }
}

int factory_process(factory_adapter *a,const uint8_t raw[CROWN_QUANTUM*CROWN_IN_STRIDE],int16_t out[CROWN_QUANTUM]) {
 if(!a || !raw || !out || a->terminal || a->closed || !a->asr) return -1;
 uint64_t sec=a->quantum_index/125u;
 if(sec>(uint64_t)INT32_MAX) {a->terminal=1;return -1;}
 asp_time32 ts={(int32_t)sec,(int32_t)((a->quantum_index%125u)*UINT64_C(8000000))};
 if(a->use_speaker) {
  upsample(a,raw);
  uint32_t si=CROWN_SPK_FRAMES,so=CROWN_SPK_FRAMES;
  asp_time32 zero={0,0};
  if(a->abi.process(a->speaker,a->ref48,&si,a->spk_out,&so,0,zero)) {
   report("crown: speaker process failed\n");a->terminal=1;return -1;
  }
 }
 static uint8_t in[CROWN_QUANTUM*CROWN_IN_STRIDE] __attribute__((aligned(16)));
 if(a->lead) {
  unsigned slots=a->lead+1;
  memcpy(a->delay[a->delay_head],raw,sizeof(in));
  unsigned oldest=(a->delay_head+1)%slots;
  memcpy(in,a->delay[oldest],sizeof(in));
  a->delay_head=oldest;
 } else {
  memcpy(in,raw,sizeof(in));
 }
 uint32_t ni=CROWN_QUANTUM,no=CROWN_QUANTUM;
 int rc=a->abi.process(a->asr,in,&ni,out,&no,0,ts);
 if(rc || no!=CROWN_QUANTUM) {
  report("crown: asr process failed\n");a->terminal=1;return -1;
 }
 ++a->quantum_index;
 return 0;
}
