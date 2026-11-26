#ifndef CROWN_FACTORY_H
#define CROWN_FACTORY_H
#include "descriptor.h"
#define CROWN_QUANTUM 128u
#define CROWN_IN_STRIDE 18u
#define CROWN_SPK_FRAMES 384u
#define CROWN_MAX_LEAD 8u
#define CROWN_DEFAULT_GAIN_DB 28
#define CROWN_LIMIT 29000
typedef struct {int32_t sec,nsec;} asp_time32;
typedef struct {uint32_t reserved;int32_t full_init;char config_dir[248];} crown_init_info;
typedef struct {
 int32_t (*init)(const crown_init_info*);
 void *(*create)(uint32_t,uint32_t,uint32_t,uint32_t,uint32_t,uint32_t,uint32_t);
 int32_t (*process)(void*,const void*,uint32_t*,void*,uint32_t*,int32_t,asp_time32);
 int32_t (*destroy)(void*);
 void (*deinit)(void);
 int32_t (*set_device)(void*,uint32_t);
 int32_t (*ref_sync)(void*,int32_t);
} factory_abi;
typedef struct {
 factory_abi abi;
 void *asr,*speaker;
 int use_speaker,initialized,terminal,closed;
 int16_t last_l,last_r;
 uint64_t quantum_index;
 unsigned lead,delay_head;
 int32_t gain_q8,limit_q15;
 uint8_t delay[CROWN_MAX_LEAD+1][CROWN_QUANTUM*CROWN_IN_STRIDE] __attribute__((aligned(16)));
 int16_t ref48[CROWN_SPK_FRAMES*2] __attribute__((aligned(16)));
 int16_t spk_out[CROWN_SPK_FRAMES*2] __attribute__((aligned(16)));
} factory_adapter;
int asp_vendor_load(factory_abi*);
void asp_vendor_unload(void);
int factory_open(factory_adapter*,const factory_abi*);
int factory_process(factory_adapter*,const uint8_t raw[CROWN_QUANTUM*CROWN_IN_STRIDE],int16_t out[CROWN_QUANTUM]);
int factory_close(factory_adapter*);
#endif
