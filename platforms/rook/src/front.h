#ifndef ROOK_FRONT_H
#define ROOK_FRONT_H
#include "compat.h"
#define TAPS 193
#define HISTORY 65
struct Upsampler {float history[2][HISTORY];u32 pos;u64 in_count,out_count;};
struct Front {struct Upsampler up;float delay[4][32];u32 dpos;u64 clips[4];};
void front_process(struct Front*,const u8*,u32,const i32*,int,float*,float*);
void strip_lsb(i16*);
#endif
