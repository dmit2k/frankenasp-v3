#ifndef CRONOS_REF_SRC_H
#define CRONOS_REF_SRC_H

#ifdef ASP_BIONIC
#include "bionic_compat.h"
#else
#include <stdint.h>
#endif

enum {
    CRONOS_ASR_FRAMES = 128,
    CRONOS_CAPTURE_STRIDE = 12,
    CRONOS_REF_CHANNELS = 2,
    CRONOS_REF_FRAMES = 3 * CRONOS_ASR_FRAMES,
    CRONOS_SRC_HISTORY = 16
};

typedef struct {
    int16_t history[CRONOS_SRC_HISTORY][CRONOS_REF_CHANNELS];
    unsigned next;
} cronos_ref_src;

void cronos_ref_src_process(cronos_ref_src *,
                            const uint8_t capture[CRONOS_ASR_FRAMES * CRONOS_CAPTURE_STRIDE],
                            int16_t ref[CRONOS_REF_FRAMES * CRONOS_REF_CHANNELS]);

#endif
