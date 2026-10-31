#include "runtime.h"
#ifdef ASP_BIONIC
#include "bionic_compat.h"
#else
#include <string.h>
#include <limits.h>
#endif
static uint16_t u16le(const uint8_t *p) { return (uint16_t)((uint16_t)p[0] | (uint16_t)p[1] << 8); }
static uint32_t u32le(const uint8_t *p) {
    return (uint32_t)p[0] | (uint32_t)p[1] << 8 | (uint32_t)p[2] << 16 | (uint32_t)p[3] << 24;
}
static int32_t s32le(const uint8_t *p) {
    uint32_t raw = u32le(p);
    int64_t signed_value = (int64_t)raw - ((raw & UINT32_C(0x80000000)) ? INT64_C(4294967296) : 0);
    return (int32_t)signed_value;
}
static int valid_order(const unsigned *slot, unsigned count) {
    unsigned seen = 0;
    for (unsigned i = 0; i < count; ++i) {
        if (slot[i] >= count || (seen & (1u << slot[i]))) return 0;
        seen |= 1u << slot[i];
    }
    return seen == (1u << count) - 1u;
}
int asp_admit(const asp_descriptor *d, const unsigned mic[4], const unsigned ref[2], asp_runtime *r) {
    if (!d || !mic || !ref || !r || !valid_order(mic, 4) || !valid_order(ref, 2) ||
        d->buffer_count != 2 || d->channel_count != 6 ||
        d->input_bytes != 38400) return -1;
    const asp_buffer *m = &d->buffers[0], *f = &d->buffers[1];
    if (m->offset != 0 || m->length != 30720 || m->sample_rate != 96000 ||
        m->frames != 1920 || m->stride != 16 || m->format != 3 ||
        m->valid_bits != 24 || (m->lsb_shift != 0 && m->lsb_shift != 8) ||
        m->channels != 4 || m->first_channel != 0 ||
        f->offset != 30720 || f->length != 7680 || f->sample_rate != 96000 ||
        f->frames != 1920 || f->stride != 4 || f->format != 1 ||
        f->valid_bits != 16 || f->lsb_shift != 0 ||
        f->channels != 2 || f->first_channel != 4) return -1;
    uint8_t mk[4] = {0}, rf[2] = {0};
    for (unsigned i = 0; i < d->channel_count; ++i) {
        const asp_channel *c = &d->channels[i];
        if (i < 4) {
            if (c->role != 1 || c->role_index >= 4 || c->buffer_index != 0 ||
                c->frame_offset >= 16 || c->frame_offset % 4) return -1;
            mk[c->role_index] = (uint8_t)i;
        } else {
            if (c->role != 2 || c->role_index >= 2 || c->buffer_index != 1 ||
                c->frame_offset >= 4 || c->frame_offset % 2) return -1;
            rf[c->role_index] = (uint8_t)i;
        }
    }
    memset(r, 0, sizeof(*r));
    r->geometry = *d;
    for (unsigned i = 0; i < 4; ++i) {
        r->mic_order[i] = (uint8_t)mic[i];
        r->mic_channel[i] = mk[i];
    }
    for (unsigned i = 0; i < 2; ++i) {
        r->ref_order[i] = (uint8_t)ref[i];
        r->ref_channel[i] = rf[i];
    }
    return 0;
}
int asp_runtime_open(asp_runtime *r, const factory_abi *abi) {
    if (!r || factory_open(&r->factory, abi)) return -1;
    r->available = 64; /* Exactly once: output FIFO only. */
    return 0;
}
static int emit_quantum(asp_runtime *r) {
    if (r->pending != 768 || r->available > 512) return -1;
    int16_t produced[128];
    if (factory_process(&r->factory, r->quantum, produced)) return -1;
    memcpy(r->fifo + r->available, produced, sizeof(produced));
    r->available += 128;
    r->pending = 0;
    ++r->quanta;
    return 0;
}
int asp_step(asp_runtime *r, const uint8_t *input, uint8_t out[ASP_OUTPUT_BYTES]) {
    if (!r || !input || !out || r->factory.closed || r->factory.terminal) return -1;
    const asp_descriptor *d = &r->geometry;
    for (unsigned frame = 0; frame < 1920; ++frame) {
        int32_t *dst = r->quantum + (size_t)r->pending * 6;
        for (unsigned i = 0; i < 4; ++i) {
            const asp_channel *c = &d->channels[r->mic_channel[r->mic_order[i]]];
            const uint8_t *p = input + d->buffers[0].offset + frame * 16u + c->frame_offset;
            int32_t v = s32le(p);
            if (d->buffers[0].lsb_shift == 8) {
                /* Arithmetic division with floor, preserving signed24 on negative samples. */
                int64_t q = v;
                v = (int32_t)(q < 0 ? -(((-q) + 255) / 256) : q / 256);
            }
            if (v < -8388608 || v > 8388607) return -1;
            dst[i] = v;
        }
        for (unsigned i = 0; i < 2; ++i) {
            const asp_channel *c = &d->channels[r->ref_channel[r->ref_order[i]]];
            const uint8_t *p = input + d->buffers[1].offset + frame * 4u + c->frame_offset;
            dst[4 + i] = (int32_t)(int16_t)u16le(p) * 65536;
        }
        if (++r->pending == 768 && emit_quantum(r)) return -1;
    }
    if (r->available < 320) return -1;
    for (unsigned i = 0; i < 320; ++i) {
        uint16_t v = (uint16_t)r->fifo[i];
        out[2*i] = (uint8_t)v;
        out[2*i + 1] = (uint8_t)(v >> 8);
    }
    r->available -= 320;
    memmove(r->fifo, r->fifo + 320, (size_t)r->available * sizeof(r->fifo[0]));
    ++r->exchanged;
    return 0;
}
int asp_runtime_close(asp_runtime *r) { return r ? factory_close(&r->factory) : -1; }
