#ifndef CRONOS_FACTORY_ADAPTER_H
#define CRONOS_FACTORY_ADAPTER_H
#include "descriptor.h"
typedef struct {
    uint32_t reserved;
    int32_t mode;
    char config[128];
} asp_init32;
_Static_assert(__builtin_offsetof(asp_init32, mode) == 4, "init mode offset");
_Static_assert(__builtin_offsetof(asp_init32, config) == 8, "inline config offset");
typedef struct {
    int32_t (*init)(const asp_init32 *);
    void *(*create)(uint32_t,uint32_t,uint32_t,uint32_t,uint32_t,uint32_t,uint32_t);
    int32_t (*process)(void *,const void *,uint32_t *,void *,uint32_t *,
                       uint32_t,uint32_t,uint32_t);
    int32_t (*destroy)(void *);
    void (*deinit)(void);
} factory_abi;
typedef struct {
    factory_abi abi;
    void *pipeline;
    uint32_t seconds, nanoseconds;
    int initialized, terminal, closed;
    struct {
        uint8_t head[32];
        uint8_t data[1536] __attribute__((aligned(16)));
        uint8_t tail[32];
    } source;
    struct {
        uint8_t head[32];
        uint8_t data[65536] __attribute__((aligned(16)));
        uint8_t tail[32];
    } output;
} factory_adapter;
int factory_open(factory_adapter *, const factory_abi *);
int factory_process(factory_adapter *, const uint8_t[1536], int16_t[128]);
int factory_close(factory_adapter *);
int asp_vendor_load(factory_abi *);
void asp_vendor_unload(void);
#endif
