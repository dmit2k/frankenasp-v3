/* Private bindings for Cronos Fire OS 6574.1, build 13222532996. */
#include "factory_adapter.h"
#include "bionic_compat.h"
#include "../../rook/src/sha256.h"

enum {
    ELF_IMAGE_VA = 0x4000,
    ASP_PROCESS_THUMB = 0x18cd1,
    PIPELINE_VTABLE = 0x1f5514,
    READER_VTABLE = 0x1f58ac,
    MANAGER_VTABLE = 0x1f5ee4,
    PIPELINE_NODES_BEGIN = 0x38,
    PIPELINE_NODES_END = 0x3c,
    READER_ID = 0x04,
    READER_CAPACITY = 0x28,
    READER_MANAGER = 0x34,
    REGISTER_SLOT = 0x0c,
    UNREGISTER_SLOT = 0x14,
    SUPPLY_SLOT = 0x1c,
    REGISTER_THUMB = 0x400a9,
    UNREGISTER_THUMB = 0x403f5,
    SUPPLY_THUMB = 0x4099d,
    WRITER_A2DP = 4,
    PCM16 = 1,
    REF_RATE = 48000
};

typedef struct {
    uint32_t format, bytes_per_sample, channels, rate, frames;
    const int16_t *pcm;
} ref_buffer32;

_Static_assert(__BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__, "factory is little endian");
_Static_assert(sizeof(ref_buffer32) == 24 &&
               __builtin_offsetof(ref_buffer32, frames) == 0x10 &&
               __builtin_offsetof(ref_buffer32, pcm) == 0x14, "factory REF descriptor");

static const char factory_path[] = "/system/lib/libasp.so";
static const char config_path[] = "/vendor/etc/audio-algorithms/asp.cfg";
static const uint8_t factory_sha[32] = {
    0xd8, 0xea, 0x58, 0x0a, 0x54, 0xfe, 0x75, 0xa5,
    0x24, 0xd5, 0x0d, 0xfe, 0xc7, 0xb8, 0xe7, 0x78,
    0xc6, 0x6c, 0xef, 0x03, 0x85, 0xa9, 0xf0, 0x14,
    0x2b, 0x84, 0xbc, 0x58, 0x31, 0x9d, 0xbf, 0xf1,
};
static const uint8_t config_sha[32] = {
    0x1f, 0x70, 0x18, 0xed, 0x34, 0x6d, 0xf6, 0x2f,
    0x86, 0xb5, 0x2d, 0x1b, 0xcb, 0x87, 0x57, 0x1d,
    0x5c, 0xb5, 0x4a, 0x91, 0x85, 0x21, 0x83, 0x56,
    0x56, 0xb2, 0xf5, 0x1c, 0x4e, 0xb3, 0xfa, 0xc7,
};
static void *library;
static uintptr_t load_bias;

static int load_one(const char *name, void *out, size_t width) {
    void *address = dlsym(library, name);
    if (!address || width != sizeof(address)) return -1;
    memcpy(out, &address, width);
    return 0;
}

static int pinned_file(const char *path, const uint8_t expected[32]) {
    uint8_t digest[32];
    return file_sha256(path, digest) == 0 && memcmp(digest, expected, sizeof(digest)) == 0;
}

static int binding_error(const char *message) {
    (void)write(2, message, strlen(message));
    return -1;
}

static uintptr_t word_at(uintptr_t address) {
    uintptr_t value;
    memcpy(&value, (const void *)address, sizeof(value));
    return value;
}

static uintptr_t find_manager(uintptr_t pipeline) {
    if (pipeline % 4 || word_at(pipeline) != load_bias + PIPELINE_VTABLE) return 0;
    /* Pipeline::Process reads this vector at 0x1c094 and dispatches each node. */
    uintptr_t begin = word_at(pipeline + PIPELINE_NODES_BEGIN);
    uintptr_t end = word_at(pipeline + PIPELINE_NODES_END);
    if (!begin || begin % 4 || end % 4 || end <= begin || end - begin > 32 * sizeof(void *))
        return 0;

    uintptr_t manager = 0;
    for (uintptr_t entry = begin; entry < end; entry += sizeof(void *)) {
        uintptr_t node = word_at(entry);
        if (!node || node % 4) return 0;
        if (word_at(node) != load_bias + READER_VTABLE) continue;
        if (manager || word_at(node + READER_ID) != 0 ||
            word_at(node + READER_CAPACITY) != 32768) return 0;
        manager = word_at(node + READER_MANAGER);
        if (!manager || manager % 4 || word_at(manager) != load_bias + MANAGER_VTABLE)
            return 0;
    }
    return manager;
}

static int32_t open_ref(void *pipeline, void **provider) {
    uintptr_t manager = find_manager((uintptr_t)pipeline);
    uintptr_t vtable = load_bias + MANAGER_VTABLE;
    if (!manager || word_at(vtable + REGISTER_SLOT) != load_bias + REGISTER_THUMB ||
        word_at(vtable + UNREGISTER_SLOT) != load_bias + UNREGISTER_THUMB ||
        word_at(vtable + SUPPLY_SLOT) != load_bias + SUPPLY_THUMB)
        return binding_error("asp1: Cronos REF object layout mismatch\n");

    int32_t (*register_writer)(void *, uint32_t, uint32_t, uint32_t, uint32_t);
    uintptr_t target = word_at(vtable + REGISTER_SLOT);
    memcpy(&register_writer, &target, sizeof(register_writer));
    int32_t rc = register_writer((void *)manager, WRITER_A2DP, PCM16, CRONOS_REF_CHANNELS, REF_RATE);
    if (rc == 0) *provider = (void *)manager;
    return rc;
}

static int32_t supply_ref(void *provider, const int16_t *pcm) {
    int32_t (*supply)(void *, uint32_t, const ref_buffer32 *,
                      uint32_t, uint32_t, uint32_t, const void *);
    uintptr_t target = load_bias + SUPPLY_THUMB;
    memcpy(&supply, &target, sizeof(supply));
    const ref_buffer32 ref = {PCM16, sizeof(*pcm), CRONOS_REF_CHANNELS,
                             REF_RATE, CRONOS_REF_FRAMES, pcm};
    /* ErWriter::Process uses zero delay/time/info for this factory interface. */
    return supply(provider, WRITER_A2DP, &ref, 0, 0, 0, NULL);
}

static int32_t close_ref(void *provider) {
    int32_t (*unregister_writer)(void *, uint32_t);
    uintptr_t target = load_bias + UNREGISTER_THUMB;
    memcpy(&unregister_writer, &target, sizeof(unregister_writer));
    return unregister_writer(provider, WRITER_A2DP);
}

int asp_vendor_load(factory_abi *v) {
    if (!v) return -1;
    memset(v, 0, sizeof(*v));
    if (!pinned_file(factory_path, factory_sha))
        return binding_error("asp1: unsupported Cronos libasp.so SHA-256\n");
    if (!pinned_file(config_path, config_sha))
        return binding_error("asp1: unsupported Cronos asp.cfg SHA-256\n");
    library = dlopen(factory_path, 2);
    if (!library) return -1;
    if (load_one("asp_parameterized_init", &v->init, sizeof(v->init)) ||
        load_one("asp_create_pipeline", &v->create, sizeof(v->create)) ||
        load_one("asp_process", &v->process, sizeof(v->process)) ||
        load_one("asp_destroy_pipeline", &v->destroy, sizeof(v->destroy)) ||
        load_one("asp_deinit", &v->deinit, sizeof(v->deinit))) {
        asp_vendor_unload();
        return -1;
    }
    asp_dl_info mapping = {0};
    void *process;
    memcpy(&process, &v->process, sizeof(process));
    if (!dladdr(process, &mapping) || !mapping.dli_fname ||
        (strcmp(mapping.dli_fname, factory_path) && strcmp(mapping.dli_fname, "libasp.so")) ||
        (uintptr_t)mapping.dli_fbase < ELF_IMAGE_VA ||
        (uintptr_t)process != (uintptr_t)mapping.dli_fbase - ELF_IMAGE_VA + ASP_PROCESS_THUMB) {
        asp_vendor_unload();
        return binding_error("asp1: Cronos libasp.so mapping mismatch\n");
    }
    load_bias = (uintptr_t)mapping.dli_fbase - ELF_IMAGE_VA;
    v->open_ref = open_ref;
    v->supply_ref = supply_ref;
    v->close_ref = close_ref;
    return 0;
}

void asp_vendor_unload(void) {
    if (library) {
        (void)dlclose(library);
        library = NULL;
    }
    load_bias = 0;
}
