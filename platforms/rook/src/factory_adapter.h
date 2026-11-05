#ifndef ROOK_FACTORY_ADAPTER_H
#define ROOK_FACTORY_ADAPTER_H
#include "descriptor.h"
typedef struct {int32_t sec,nsec;} asp_time32;
typedef struct {uintptr_t reserved;int32_t register_binder_service;} asp_init32;
typedef struct {void *provider;uint32_t pcm_format,channels,sample_rate,source_mode;} rook_writer32;
_Static_assert(sizeof(asp_time32)==8,"timespec ABI");
typedef struct {
 int32_t (*init)(const asp_init32*);
 void *(*create)(uint32_t,uint32_t,uint32_t,uint32_t,uint32_t,uint32_t,uint32_t);
 int32_t (*ref_sync)(void*,int32_t);
 int32_t (*process)(void*,const void*,uint32_t*,void*,uint32_t*,int32_t,asp_time32);
 int32_t (*destroy)(void*);
 void (*deinit)(void);
 void (*unpack)(uint32_t[8],const void*,uint32_t);
 void (*pack)(uint32_t[8],void*,uint32_t);
 int32_t (*register_writer)(void*,rook_writer32*);
 int32_t (*unregister_writer)(void*,rook_writer32*);
 int32_t (*push)(void*,rook_writer32*,const int16_t*,uint32_t,int32_t,int32_t,int32_t,const void*);
 uintptr_t expected_vtable;
} factory_abi;
typedef struct {
 factory_abi abi;
 void *pipeline,*provider;
 rook_writer32 writer;
 struct {
  uint8_t head[32];
  int32_t data[6*128] __attribute__((aligned(16)));
  uint8_t tail[32];
 } planar;
 struct {
  uint8_t head[32];
  int16_t data[128*2] __attribute__((aligned(16)));
  uint8_t tail[32];
 } reference;
 uint64_t quantum_index;
 int initialized,registered,terminal,closed;
} factory_adapter;
int factory_open(factory_adapter*,const factory_abi*);
int factory_process(factory_adapter*,const uint8_t[2304],int16_t[128]);
int factory_close(factory_adapter*);
int asp_vendor_load(factory_abi*);
void asp_vendor_unload(void);
#endif
