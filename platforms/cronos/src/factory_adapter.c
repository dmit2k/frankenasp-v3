/* Original Cronos ASR with a session-owned native writer4 registration. */
#include "factory_adapter.h"
#ifdef ASP_BIONIC
#include "bionic_compat.h"
#else
#include <string.h>
#include <limits.h>
#endif

int factory_close(factory_adapter *a) {
    if (!a || a->closed) return 0;
    a->closed = 1;
    int result = 0;
    if (a->provider) {
        void *provider = a->provider;
        a->provider = NULL;
        if (a->abi.close_ref(provider) != 0) result = -1;
    }
    if (a->pipeline) {
        void *pipeline = a->pipeline;
        a->pipeline = NULL;
        if (a->abi.destroy(pipeline) != 0) result = -1;
    }
    if (a->initialized) {
        a->initialized = 0;
        a->abi.deinit();
    }
    return result;
}

int factory_open(factory_adapter *a, const factory_abi *abi) {
    static const char config[] = "/vendor/etc/audio-algorithms//asp.cfg";
    if (!a) return -1;
    memset(a, 0, sizeof(*a));
    if (!abi || !abi->init || !abi->create || !abi->process ||
        !abi->destroy || !abi->deinit || !abi->open_ref ||
        !abi->supply_ref || !abi->close_ref) {
        a->terminal = 1;
        return -1;
    }
    a->abi = *abi;
    /* Proven accessed fields; this allocation is not a claimed factory sizeof. */
    asp_init32 params = {0};
    memcpy(params.config, config, sizeof(config));
    if (a->abi.init(&params) != 0) {
        a->terminal = 1;
        return -1;
    }
    a->initialized = 1;
    a->pipeline = a->abi.create(0, 6, 4, 16000, 1, 1, 16000);
    void *provider = NULL;
    if (!a->pipeline || a->abi.open_ref(a->pipeline, &provider) != 0 || !provider) {
        a->terminal = 1;
        (void)factory_close(a);
        return -1;
    }
    a->provider = provider;
    return 0;
}

static int intact(const uint8_t *bytes, size_t length) {
    for (size_t i = 0; i < length; ++i)
        if (bytes[i] != 0xa5) return 0;
    return 1;
}

int factory_process(factory_adapter *a, const uint8_t in[1536], int16_t out[128]) {
    if (!a || !in || !out || a->terminal || a->closed || !a->provider) return -1;
    if (a->seconds > (uint32_t)INT32_MAX || a->nanoseconds >= 1000000000u ||
        (a->seconds == (uint32_t)INT32_MAX && a->nanoseconds >= 992000000u)) {
        a->terminal = 1;
        return -1;
    }
    memset(&a->source, 0xa5, sizeof(a->source));
    memcpy(a->source.data, in, sizeof(a->source.data));
    memset(&a->reference, 0xa5, sizeof(a->reference));
    cronos_ref_src_process(&a->ref_src, a->source.data, a->reference.pcm);
    if (a->abi.supply_ref(a->provider, a->reference.pcm) != 0 ||
        !intact(a->reference.head, sizeof(a->reference.head)) ||
        !intact(a->reference.tail, sizeof(a->reference.tail))) {
        a->terminal = 1;
        return -1;
    }
    memset(&a->output, 0xa5, sizeof(a->output));
    memset(a->output.data, 0, 256);
    uint32_t consumed = 128, produced = 128;
    /* Logical stream time only: not a claim about factory hardware timestamps. */
    int rc = a->abi.process(a->pipeline, a->source.data, &consumed,
                            a->output.data, &produced, 0,
                            a->seconds, a->nanoseconds);
    if (rc != 0 || consumed != 128 || produced != 128 ||
        !intact(a->source.head, sizeof(a->source.head)) ||
        !intact(a->source.tail, sizeof(a->source.tail)) ||
        memcmp(a->source.data, in, sizeof(a->source.data)) != 0 ||
        !intact(a->output.head, sizeof(a->output.head)) ||
        !intact(a->output.data + 256, sizeof(a->output.data) - 256) ||
        !intact(a->output.tail, sizeof(a->output.tail))) {
        a->terminal = 1;
        return -1;
    }
    memcpy(out, a->output.data, 256);
    a->nanoseconds += 8000000u;
    if (a->nanoseconds >= 1000000000u) {
        ++a->seconds;
        a->nanoseconds -= 1000000000u;
    }
    return 0;
}
