#include "ref_src.h"

/* Three phases of the validated 49-tap Hann-windowed sinc, in Q30.
 * Phase 0 is the exact sample delayed by eight input frames. */
static const int32_t taps[2][CRONOS_SRC_HISTORY] = {
    {-165099, 2973285, -10214964, 23498509, -45617557, 83223381, -159198857, 436295984,
     883918796, -207062591, 102008812, -55873694, 29686379, -13870563, 4827462, -687459},
    {-687459, 4827462, -13870563, 29686379, -55873694, 102008812, -207062591, 883918796,
     436295984, -159198857, 83223381, -45617557, 23498509, -10214964, 2973285, -165099}
};

static int16_t high16(const uint8_t *packed24) {
    int32_t sample = (int32_t)packed24[1] | (int32_t)packed24[2] << 8;
    if (sample >= 32768) sample -= 65536;
    return (int16_t)sample;
}

static int16_t pcm16_from_q30(int64_t value) {
    const int64_t scale = INT64_C(1) << 30;
    /* Round to nearest, with exact ties toward positive infinity. */
    value += scale / 2;
    int64_t sample = value >= 0 ? value / scale : -((-value + scale - 1) / scale);
    if (sample > 32767) return 32767;
    if (sample < -32768) return -32768;
    return (int16_t)sample;
}

void cronos_ref_src_process(cronos_ref_src *s,
                            const uint8_t capture[CRONOS_ASR_FRAMES * CRONOS_CAPTURE_STRIDE],
                            int16_t ref[CRONOS_REF_FRAMES * CRONOS_REF_CHANNELS]) {
    const unsigned mask = CRONOS_SRC_HISTORY - 1;
    for (unsigned frame = 0; frame < CRONOS_ASR_FRAMES; ++frame) {
        const uint8_t *loopback = capture + frame * CRONOS_CAPTURE_STRIDE + 6;
        int16_t *out = ref + frame * 3 * CRONOS_REF_CHANNELS;
        unsigned current = s->next;

        for (unsigned ch = 0; ch < CRONOS_REF_CHANNELS; ++ch) {
            s->history[current][ch] = high16(loopback + 3 * ch);
            out[ch] = s->history[(current - 8) & mask][ch];
            int64_t phase1 = 0, phase2 = 0;
            for (unsigned lag = 0; lag < CRONOS_SRC_HISTORY; ++lag) {
                int16_t sample = s->history[(current - lag) & mask][ch];
                phase1 += (int64_t)taps[0][lag] * sample;
                phase2 += (int64_t)taps[1][lag] * sample;
            }
            out[CRONOS_REF_CHANNELS + ch] = pcm16_from_q30(phase1);
            out[2 * CRONOS_REF_CHANNELS + ch] = pcm16_from_q30(phase2);
        }
        s->next = (current + 1) & mask;
    }
}
