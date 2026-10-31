/* Independent six-export vendor ABI mock. Never linked into production. */
#include "../src/factory_adapter.h"
#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <string.h>
#include <sys/stat.h>
static unsigned init_calls, create_calls, ref_calls, process_calls, destroy_calls, deinit_calls;
static int pipeline_sentinel;
static int fault(const char *name) {
    const char *mode = getenv("ASP1_TEST_FAULT");
    return mode && !strcmp(mode, name);
}
static int32_t mock_init(void) {
    ++init_calls;
    puts("mock vendor stdout chatter, must not appear in wire");
    const char *resource = getenv("ASP1_TEST_RESOURCE");
    struct stat st;
    if (!resource || stat(resource, &st) || !S_ISREG(st.st_mode) || fault("init")) return -1;
    return 0;
}
static void *mock_create(uint32_t a,uint32_t mic,uint32_t channels,uint32_t rate,
                         uint32_t one,uint32_t again,uint32_t output) {
    ++create_calls;
    if (a || mic != 4 || channels != 6 || rate != 96000 || one != 1 ||
        again != 1 || output != 16000 || fault("create")) return NULL;
    return &pipeline_sentinel;
}
static int32_t mock_ref(void *p, int32_t ref) {
    ++ref_calls;
    return p != &pipeline_sentinel || ref != 2 || fault("ref") ? -1 : 0;
}
static int32_t mock_process(void *p,const void *v,uint32_t *n,void *output,
                            uint32_t *n_out,int32_t unused,asp_time32 stamp) {
    (void)unused;
    ++process_calls;
    if (p != &pipeline_sentinel || *n != 768 || *n_out != 128 ||
        stamp.sec < 0 || stamp.nsec < 0 || stamp.nsec >= 1000000000 || fault("process")) return -1;
    const int32_t *src = v;
    int16_t *dst = output;
    /* Sensitive to distinct MIC0/MIC1 and REF L/R ordering; no calibration multiplication. */
    int64_t x = (int64_t)src[0]/64 + (int64_t)src[1]/128 +
                (int64_t)src[4]/65536 - (int64_t)src[5]/65536;
    int16_t value = (int16_t)((x % 16000) + 120);
    for (unsigned i = 0; i < 128; ++i) dst[i] = value;
    if (fault("counts")) --*n_out;
    if (fault("overrun")) ((uint8_t*)output)[256] = 0;
    return 0;
}
static int32_t mock_destroy(void *p) { ++destroy_calls; return p != &pipeline_sentinel; }
static void mock_deinit(void) { ++deinit_calls; }
int asp_vendor_load(factory_abi *out) {
    *out = (factory_abi){mock_init,mock_create,mock_ref,mock_process,mock_destroy,mock_deinit};
    return 0;
}
void asp_vendor_unload(void) {
    fprintf(stderr, "mock counts init=%u create=%u ref=%u process=%u destroy=%u deinit=%u\n",
            init_calls,create_calls,ref_calls,process_calls,destroy_calls,deinit_calls);
}
