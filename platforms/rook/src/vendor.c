/* Pinned factory ASR adapter for rook build 690918120, ARM32 Bionic API 22. */
#include "vendor.h"
#include "front.h"
#include "core.h"
#include "runtime_manifest.h"
#ifndef ROOK_TEST_ONLY
extern void *dlopen(const char *, int);
extern void *dlsym(void *, const char *);
struct DlInfo { const char *name; void *base; const char *symbol; void *symbol_address; };
extern int dladdr(const void *, struct DlInfo *);
#endif

struct Buffer32 {
    u32 vptr, channels, stride, capacity, frames, interleaved, owns;
    void *data;
};
_Static_assert(sizeof(struct Buffer32) == 32, "factory ARM32 buffer ABI");
struct FloatGuard { u32 head[4]; float pcm[768]; u32 tail[4]; };
struct PostGuard { u32 head[4]; float pcm[512]; u32 tail[4]; };
struct PcmGuard { u32 head[4]; i16 pcm[128]; u32 tail[4]; };
static struct FloatGuard mic __attribute__((aligned(16)));
static struct FloatGuard ref __attribute__((aligned(16)));
static struct PostGuard post __attribute__((aligned(16)));
static struct PcmGuard result __attribute__((aligned(16)));
static struct Front frontend;
static struct Buffer32 mic_buffer, ref_buffer, out_buffer, post_buffer;
static void *pipeline, *afe;
static void (*process)(void *, struct Buffer32 *, struct Buffer32 *,
                       struct Buffer32 *, struct Buffer32 *, int);
static int (*destroy)(void *);
static void (*deinit)(void);
static int active;

static void init_guard(u32 a[4], u32 b[4]) {
    for (unsigned i = 0; i < 4; ++i) a[i] = b[i] = 0x9a63c57eU;
}
static int guard_ok(const u32 a[4], const u32 b[4]) {
    for (unsigned i = 0; i < 4; ++i)
        if (a[i] != 0x9a63c57eU || b[i] != 0x9a63c57eU) return 0;
    return 1;
}
static int shape(struct Buffer32 b, u32 channels, u32 frames, u32 size) {
    return b.channels == channels && b.frames == frames &&
           b.stride == frames && b.capacity == size && !b.interleaved;
}
int rook_factory_open(void) {
#ifdef ROOK_TEST_ONLY
    return -1; /* No vendor simulation can be used in a production binary. */
#else
    if (active) return -1;
    u8 actual[32];
    for (usize i = 0; i < RUNTIME_COUNT; ++i)
        if (file_sha256(runtime_manifest[i].path, actual) ||
            memcmp(actual, runtime_manifest[i].sha, 32)) return -1;
    void *library = dlopen("/system/lib/libasp.so", 2);
    if (!library) return -1;
    int (*init)(void *) = dlsym(library, "asp_parameterized_init");
    void *(*create)(u32,u32,u32,u32,u32,u32,u32) =
        dlsym(library, "asp_create_pipeline");
    destroy = dlsym(library, "asp_destroy_pipeline");
    deinit = dlsym(library, "asp_deinit");
    int (*command)(void *, int, u32, void *, u32 *, void *) =
        dlsym(library, "asp_command");
    if (!init || !create || !destroy || !deinit || !command) return -1;
    u32 params[2] = {0,0}; /* Binder registration disabled. */
    if (init(params)) return -1;
    pipeline = create(0, 4, 6, 16000, 1, 1, 16000);
    if (!pipeline) return -1;
    afe = *(void **)((u8 *)pipeline + 4);
    if (!afe) return -1;
    process = (void *)(*(u32 **)afe)[8];
    struct DlInfo owner;
    if (!dladdr((void *)create, &owner) ||
        ((uptr)process & ~(uptr)1) != (uptr)owner.base + 0x3bda0U) return -1;
    mic_buffer = *(struct Buffer32 *)((u8 *)pipeline + 0x60);
    ref_buffer = *(struct Buffer32 *)((u8 *)pipeline + 0xc0);
    out_buffer = *(struct Buffer32 *)((u8 *)pipeline + 0xe0);
    post_buffer = *(struct Buffer32 *)((u8 *)pipeline + 0x100);
    if (!shape(mic_buffer, 6, 128, 768) ||
        !shape(ref_buffer, 2, 384, 768) ||
        !shape(out_buffer, 1, 128, 128) ||
        !shape(post_buffer, 4, 128, 512)) return -1;
    mic_buffer.data = mic.pcm;
    ref_buffer.data = ref.pcm;
    out_buffer.data = result.pcm;
    post_buffer.data = post.pcm;
    u32 enabled = 1, volume = 50;
    if (command(0, 0, 4, &enabled, 0, 0) ||
        command(0, 5, 4, &volume, 0, 0)) return -1;
    active = 1;
    return 0;
#endif
}
int rook_factory_process(const u8 input[ROOK_CAPTURE_BYTES],
                         const i32 gains[4], i16 output[ROOK_QUANTUM]) {
    if (!active || !input || !gains || !output) return -1;
    init_guard(mic.head, mic.tail);
    init_guard(ref.head, ref.tail);
    init_guard(post.head, post.tail);
    init_guard(result.head, result.tail);
    front_process(&frontend, input, ROOK_QUANTUM, gains, 1, mic.pcm, ref.pcm);
    process(afe, &mic_buffer, &ref_buffer, &out_buffer, &post_buffer, 1);
    if (!guard_ok(mic.head, mic.tail) || !guard_ok(ref.head, ref.tail) ||
        !guard_ok(post.head, post.tail) || !guard_ok(result.head, result.tail))
        return -1;
    strip_lsb(result.pcm); /* Factory mode-7 transport metadata is not audio. */
    memcpy(output, result.pcm, ROOK_QUANTUM * sizeof(i16));
    return 0;
}
int rook_factory_close(void) {
    if (!pipeline) return -1;
    int rc = destroy(pipeline);
    deinit();
    pipeline = 0;
    active = 0;
    return rc ? -1 : 0;
}
