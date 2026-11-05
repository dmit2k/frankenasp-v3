#ifndef ASP1_RUNTIME_H
#define ASP1_RUNTIME_H
#include "descriptor.h"
#include "factory_adapter.h"
typedef struct {
 asp_descriptor geometry;
 uint8_t quantum[2304] __attribute__((aligned(16)));
 int16_t fifo[640];
 unsigned pending,available;
 uint64_t quanta,exchanged;
 factory_adapter factory;
} asp_runtime;
int asp_admit(const asp_descriptor*,asp_runtime*);
int asp_runtime_open(asp_runtime*,const factory_abi*);
int asp_step(asp_runtime*,const uint8_t*,uint8_t[ASP_OUTPUT_BYTES]);
int asp_runtime_close(asp_runtime*);
#endif
