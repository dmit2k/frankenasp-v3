/* Cronos: byte-exact four-channel capture; no helper-side DSP or MIC mapping. */
#include "runtime.h"
#ifdef ASP_BIONIC
#include "bionic_compat.h"
#else
#include <string.h>
#endif
int asp_admit(const asp_descriptor *d,asp_runtime *r) {
 if(!d || !r || d->buffer_count!=1 || d->channel_count!=4 || d->input_bytes!=3840) return -1;
 const asp_buffer *b=&d->buffers[0];
 if(b->offset || b->length!=3840 || b->sample_rate!=16000 || b->frames!=320 ||
    b->stride!=12 || b->format!=2 || b->valid_bits!=24 || b->lsb_shift ||
    b->channels!=4 || b->first_channel) return -1;
 for(unsigned i=0;i<4;i++) {
  const asp_channel *c=&d->channels[i];
  if(c->buffer_index || c->role!=(i<2?1:2) ||
     c->role_index!=(i<2?i:i-2) || c->frame_offset!=i*3u) return -1;
 }
 memset(r,0,sizeof(*r));r->geometry=*d;return 0;
}
int asp_runtime_open(asp_runtime *r,const factory_abi *abi) {
 if(!r || factory_open(&r->factory,abi)) return -1;
 r->available=64; /* One initial packetization pad, not an acoustic delay. */
 return 0;
}
static int emit_quantum(asp_runtime *r) {
 if(r->pending!=128 || r->available>512) return -1;
 int16_t produced[128];
 if(factory_process(&r->factory,r->quantum,produced)) return -1;
 memcpy(r->fifo+r->available,produced,sizeof(produced));
 r->available+=128;r->pending=0;++r->quanta;return 0;
}
int asp_step(asp_runtime *r,const uint8_t *input,uint8_t out[ASP_OUTPUT_BYTES]) {
 if(!r || !input || !out || r->factory.closed || r->factory.terminal) return -1;
 for(unsigned frame=0;frame<320;frame++) {
  memcpy(r->quantum+(size_t)r->pending*12u,input+(size_t)frame*12u,12);
  if(++r->pending==128 && emit_quantum(r)) return -1;
 }
 if(r->available<320) return -1;
 for(unsigned i=0;i<320;i++) {
  uint16_t v=(uint16_t)r->fifo[i];out[i*2u]=(uint8_t)v;out[i*2u+1u]=(uint8_t)(v>>8);
 }
 r->available-=320;
 memmove(r->fifo,r->fifo+320,(size_t)r->available*sizeof(r->fifo[0]));
 ++r->exchanged;return 0;
}
int asp_runtime_close(asp_runtime *r) { return r?factory_close(&r->factory):-1; }
