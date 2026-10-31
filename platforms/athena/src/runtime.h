#ifndef ASP1_RUNTIME_H
#define ASP1_RUNTIME_H
#include "descriptor.h"
#include "factory_adapter.h"
typedef struct {
    asp_descriptor geometry;
    uint8_t mic_channel[4], ref_channel[2];
    uint8_t mic_order[4], ref_order[2];
    int32_t quantum[768 * 6] __attribute__((aligned(16)));
    int16_t fifo[640];
    unsigned pending, available;
    uint64_t quanta, exchanged;
    factory_adapter factory;
} asp_runtime;
/* Only the previously qualified factory geometry; valid generic codec != DSP admission. */
int asp_admit(const asp_descriptor *d, const unsigned mic[4], const unsigned ref[2], asp_runtime *r);
int asp_runtime_open(asp_runtime *r, const factory_abi *abi);
int asp_step(asp_runtime *r, const uint8_t *input, uint8_t output[ASP_OUTPUT_BYTES]);
int asp_runtime_close(asp_runtime *r);
#endif
