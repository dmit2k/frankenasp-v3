#include "factory_adapter.h"
#ifdef ASP_BIONIC
#include "bionic_compat.h"
#else
#include <stdint.h>
#include <limits.h>
#include <string.h>
#endif

int factory_close(factory_adapter *a) {
    if (!a || a->closed) return 0;
    a->closed = 1;
    int result = 0;
    if (a->pipeline) {
        void *p = a->pipeline;
        a->pipeline = NULL;
        if (a->abi.destroy(p) != 0) result = -1;
    }
    if (a->initialized) {
        a->initialized = 0;
        a->abi.deinit();
    }
    return result;
}
int factory_open(factory_adapter *a, const factory_abi *v) {
    if (!a) return -1;
    memset(a, 0, sizeof(*a));
    if (!v || !v->init || !v->create || !v->ref_sync || !v->process ||
        !v->destroy || !v->deinit) { a->terminal = 1; return -1; }
    a->abi = *v;
    if (a->abi.init() != 0) { a->terminal = 1; return -1; }
    a->initialized = 1;
    a->pipeline = a->abi.create(0, 4, 6, 96000, 1, 1, 16000);
    if (!a->pipeline || a->abi.ref_sync(a->pipeline, 2) != 0) {
        a->terminal = 1;
        (void)factory_close(a);
        return -1;
    }
    return 0;
}
static int intact(const uint8_t *p, unsigned count) {
    for (unsigned i = 0; i < count; ++i) if (p[i] != 0xa5) return 0;
    return 1;
}
int factory_process(factory_adapter *a, const int32_t in[768 * 6], int16_t out[128]) {
    struct { uint8_t guard[32]; int32_t data[768*6] __attribute__((aligned(16)));
             uint8_t tail[32]; } src;
    struct { uint8_t guard[32]; int16_t data[128] __attribute__((aligned(16)));
             uint8_t tail[32]; } dst;
    if (!a || !in || !out || a->terminal || a->closed || !a->pipeline) return -1;
    if (a->stamp.sec < 0 || a->stamp.nsec < 0 ||
        a->stamp.nsec >= 1000000000 ||
        (a->stamp.sec == INT32_MAX && a->stamp.nsec >= 992000000)) {
        a->terminal = 1; return -1;
    }
    memset(&src, 0xa5, sizeof(src));
    memcpy(src.data, in, sizeof(src.data));
    memset(&dst, 0xa5, sizeof(dst));
    memset(dst.data, 0, sizeof(dst.data));
    uint32_t ni = 768, no = 128;
    int rc = a->abi.process(a->pipeline, src.data, &ni, dst.data, &no, 0, a->stamp);
    if (rc != 0 || ni != 768 || no != 128 || !intact(src.guard, 32) ||
        !intact(src.tail, 32) || !intact(dst.guard, 32) || !intact(dst.tail, 32) ||
        memcmp(src.data, in, sizeof(src.data)) != 0) {
        a->terminal = 1; return -1;
    }
    memcpy(out, dst.data, sizeof(dst.data));
    a->stamp.nsec += 8000000;
    if (a->stamp.nsec >= 1000000000) {
        ++a->stamp.sec;
        a->stamp.nsec -= 1000000000;
    }
    return 0;
}
