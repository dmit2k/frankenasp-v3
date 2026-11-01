#include "front.h"
#include "resampler_coeffs.h"
i32 pcm24(const u8*p){u32 x=(u32)p[0]|(u32)p[1]<<8|(u32)p[2]<<16;return (i32)(x^0x800000u)-8388608;}
float calibrated(i32 v,i32 gain,u32*clip){float x=(float)v*(1.0f/8388608.0f)*((float)gain*(1.0f/1073741824.0f));if(x>=1){x=8388607.0f/8388608.0f;(*clip)++;}if(x< -1){x=-1;(*clip)++;}return x;}
void upsample(struct Upsampler*s,const float*in,u32 n,float*out){
 for(u32 i=0;i<n;i++){u32 at=s->pos;for(u32 c=0;c<2;c++)s->history[c][at]=in[c*n+i];
  for(u32 c=0;c<2;c++)for(u32 phase=0;phase<3;phase++){float y=0;u32 idx=at;
   for(u32 j=0;3*j+phase<TAPS;j++){y+=interpolation_fir[3*j+phase]*s->history[c][idx];idx=idx?idx-1:HISTORY-1;}
   out[c*(n*3)+3*i+phase]=y;
  }s->pos=(at+1)%HISTORY;
 }s->in_count+=n;s->out_count+=(u64)n*3;
}
void front_process(struct Front*s,const u8*raw,u32 n,const i32*g,int refvalid,float*mic,float*ref){
 float r[2*128];memset(mic,0,6*n*sizeof(float));
 for(u32 i=0;i<n;i++){for(u32 c=0;c<4;c++){u32 clip=0;float x=calibrated(pcm24(raw+(i*6+c)*3),g[c],&clip);s->clips[c]+=clip;
   mic[c*n+i]=s->delay[c][s->dpos];s->delay[c][s->dpos]=x;}
  s->dpos=(s->dpos+1)%32;
  for(u32 c=0;c<2;c++)r[c*n+i]=refvalid?(float)pcm24(raw+(i*6+4+c)*3)*(1.0f/8388608.0f):0;
 }upsample(&s->up,r,n,ref);
}
void strip_lsb(i16*p){for(u32 i=0;i<128;i++)p[i]=(i16)((u16)p[i]&0xfffe);}
