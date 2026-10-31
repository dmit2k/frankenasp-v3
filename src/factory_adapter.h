#ifndef ASP1_FACTORY_ADAPTER_H
#define ASP1_FACTORY_ADAPTER_H
#include "descriptor.h"
typedef struct { int32_t sec, nsec; } asp_time32;
_Static_assert(sizeof(asp_time32) == 8, "ARM32 timespec ABI is 8 bytes");
typedef struct {
    int32_t (*init)(void);
    void *(*create)(uint32_t,uint32_t,uint32_t,uint32_t,uint32_t,uint32_t,uint32_t);
    int32_t (*ref_sync)(void *,int32_t);
    int32_t (*process)(void *,const void *,uint32_t *,void *,uint32_t *,int32_t,asp_time32);
    int32_t (*destroy)(void *);
    void (*deinit)(void);
} factory_abi;
typedef struct {
    factory_abi abi;
    void *pipeline;
    asp_time32 stamp;
    int initialized, terminal, closed;
} factory_adapter;
int factory_open(factory_adapter *a, const factory_abi *v);
int factory_process(factory_adapter *a, const int32_t in[768 * 6], int16_t out[128]);
int factory_close(factory_adapter *a);
/* Production has the fixed libasp.so binding, test has independent mock ABI. */
int asp_vendor_load(factory_abi *abi);
void asp_vendor_unload(void);
#endif
