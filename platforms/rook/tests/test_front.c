#include "../src/front.h"
#include <assert.h>
#include <math.h>
#include <stdio.h>
#include <string.h>
static void sample(u8 *dst, i32 value) {
    u32 v = (u32)value;
    dst[0] = (u8)v; dst[1] = (u8)(v >> 8); dst[2] = (u8)(v >> 16);
}
static void pack(u8 *dst, unsigned frames) {
    const i32 slots[6] = {100000, -100000, 8388607, -8388608, 200000, 0};
    for (unsigned f = 0; f < frames; ++f)
        for (unsigned c = 0; c < 6; ++c) sample(dst + (f*6 + c)*3, slots[c]);
}
static float distance(float a, float b) { return fabsf(a - b); }
int main(void) {
    u8 input[128*18]; pack(input, 128);
    i32 gains[4] = {1073741824,1073741824,1073741824,1073741824};
    struct Front full = {0}, split = {0};
    float mic_full[6*128], ref_full[2*384], mic_part[6*64], ref_part[2*192];
    front_process(&full, input, 128, gains, 1, mic_full, ref_full);
    for (unsigned c = 0; c < 4; ++c)
        for (unsigned f = 0; f < 32; ++f) assert(mic_full[c*128+f] == 0.0f);
    assert(distance(mic_full[0*128+32], 100000.0f/8388608.0f) < 0.000001f);
    assert(distance(mic_full[1*128+32], -100000.0f/8388608.0f) < 0.000001f);
    assert(mic_full[2*128+32] > 0.99f);
    assert(mic_full[3*128+32] == -1.0f);
    for (unsigned f = 0; f < 128; ++f) {
        assert(mic_full[4*128+f] == 0.0f);
        assert(mic_full[5*128+f] == 0.0f);
    }
    float left = 0.0f;
    for (unsigned i = 0; i < 384; ++i) {
        left += fabsf(ref_full[i]); assert(ref_full[384+i] == 0.0f);
    }
    assert(left > 0.1f);
    for (unsigned batch = 0; batch < 2; ++batch) {
        front_process(&split, input + batch*64*18, 64, gains, 1, mic_part, ref_part);
        for (unsigned c = 0; c < 6; ++c)
            for (unsigned f = 0; f < 64; ++f)
                assert(distance(mic_part[c*64+f], mic_full[c*128+batch*64+f]) < 0.000001f);
        for (unsigned c = 0; c < 2; ++c)
            for (unsigned f = 0; f < 192; ++f)
                assert(distance(ref_part[c*192+f], ref_full[c*384+batch*192+f]) < 0.000001f);
    }
    assert(full.up.in_count == 128 && full.up.out_count == 384);
    assert(split.up.in_count == 128 && split.up.out_count == 384);
    i16 metadata[128] = {3,-3,32767,-32767}; strip_lsb(metadata);
    assert(metadata[0] == 2 && metadata[1] == -4 && metadata[2] == 32766 && metadata[3] == -32768);
    puts("rook frontend: PCM24, mic/ref separation, FIR continuity and metadata PASS");
}
