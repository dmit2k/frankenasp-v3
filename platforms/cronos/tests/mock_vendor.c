/* Host-only independent factory API oracle; never linked into production. */
#include "../src/factory_adapter.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
static unsigned init_n, create_n, process_n, destroy_n, deinit_n;
static unsigned ref_open_n, ref_supply_n, ref_close_n;
static int pipeline, provider, ref_registered;
static int fault(const char *name) {
    const char *value = getenv("ASP1_TEST_FAULT");
    return value && !strcmp(value, name);
}
static uint32_t sample24(uint64_t frame, unsigned ch) {
    static const uint32_t edges[] = {0, 1, 255, 256, 0x7fffff, 0x800000, 0xffff00, 0xffffff};
    if (frame % 37u < 8u) return edges[(frame + ch) % 8u];
    return (uint32_t)((frame * 104729u + ch * 99991u + 0x123457u) & 0xffffffu);
}
static int32_t mock_init(const asp_init32 *args) {
    ++init_n;
    puts("mock vendor stdout chatter");
    const char *resource = getenv("ASP1_TEST_RESOURCE");
    struct stat st;
    if (!args || args->reserved || args->mode ||
        strcmp(args->config, "/vendor/etc/audio-algorithms//asp.cfg") ||
        !resource || stat(resource, &st) || !S_ISREG(st.st_mode) || fault("init")) return -1;
    return 0;
}
static void *mock_create(uint32_t type, uint32_t fmt, uint32_t ch, uint32_t rate,
                          uint32_t outfmt, uint32_t outch, uint32_t outrate) {
    ++create_n;
    return type || fmt != 6 || ch != 4 || rate != 16000 || outfmt != 1 ||
        outch != 1 || outrate != 16000 || fault("create") ? NULL : &pipeline;
}
static int32_t mock_ref_open(void *handle, void **ref) {
    ++ref_open_n;
    if (handle != &pipeline || ref_registered || fault("ref-open")) return -1;
    ref_registered = 1;
    *ref = &provider;
    return 0;
}
static int32_t mock_ref_supply(void *ref, const int16_t *pcm) {
    ++ref_supply_n;
    if (ref != &provider || !ref_registered || !pcm ||
        ref_supply_n != process_n + 1 || fault("ref-supply")) return -1;
    if (fault("ref-overrun")) ((int16_t *)(uintptr_t)pcm)[CRONOS_REF_FRAMES * CRONOS_REF_CHANNELS] = 0;
    return 0;
}
static int32_t mock_ref_close(void *ref) {
    ++ref_close_n;
    if (ref != &provider || !ref_registered) return -1;
    ref_registered = 0;
    return fault("ref-close") ? -1 : 0;
}
static int32_t mock_process(void *handle, const void *input, uint32_t *ni,
                             void *output, uint32_t *no, uint32_t context,
                             uint32_t sec, uint32_t nsec) {
    unsigned q = process_n++;
    if (handle != &pipeline || !ref_registered || ref_supply_n != q + 1 ||
        !input || !output || !ni || !no ||
        *ni != 128 || *no != 128 || context ||
        sec != q / 125u || nsec != (q % 125u) * 8000000u || fault("process")) return -1;
    const uint8_t *raw = input;
    for (unsigned f = 0; f < 128; ++f) {
        for (unsigned ch = 0; ch < 4; ++ch) {
            const uint8_t *s = raw + f * 12u + ch * 3u;
            uint32_t actual = (uint32_t)s[0] | (uint32_t)s[1] << 8 | (uint32_t)s[2] << 16;
            if (getenv("ASP1_TEST_SEQUENCE") && actual != sample24((uint64_t)q * 128u + f, ch)) return -1;
        }
        int16_t value = (int16_t)((int32_t)((q * 128u + f) % 30001u) - 15000);
        memcpy((uint8_t *)output + f * 2u, &value, 2);
    }
    if (fault("counts")) --*no;
    if (fault("input-count")) --*ni;
    if (fault("overrun")) ((uint8_t *)output)[256] = 0;
    if (fault("far-overrun")) ((uint8_t *)output)[65536] = 0;
    if (fault("mutate")) ((uint8_t *)(uintptr_t)input)[0] ^= 1;
    if (fault("input-overrun")) ((uint8_t *)(uintptr_t)input)[1536] = 0;
    return 0;
}
static int32_t mock_destroy(void *p) {
    ++destroy_n;
    return p != &pipeline || ref_registered || fault("destroy");
}
static void mock_deinit(void) { ++deinit_n; }
int asp_vendor_load(factory_abi *out) {
    if (fault("load")) return -1;
    *out = (factory_abi){mock_init, mock_create, mock_process, mock_destroy, mock_deinit,
                         mock_ref_open, mock_ref_supply, mock_ref_close};
    return 0;
}
void asp_vendor_unload(void) {
    fprintf(stderr, "mock counts init=%u create=%u process=%u destroy=%u deinit=%u "
                    "ref_open=%u ref_supply=%u ref_close=%u\n",
            init_n, create_n, process_n, destroy_n, deinit_n, ref_open_n, ref_supply_n, ref_close_n);
}
