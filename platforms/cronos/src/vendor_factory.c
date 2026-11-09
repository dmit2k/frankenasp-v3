/* Actual factory ABI binding: Bionic runtime only. NEVER a mock fallback. */
#include "factory_adapter.h"
#ifdef ASP_BIONIC
#include "bionic_compat.h"
#else
#error "Production vendor binding must be ARM32 Bionic"
#endif
static void *library;
static int load_one(void *lib, const char *name, void *out, size_t width) {
    void *address = dlsym(lib, name);
    if (!address || width != sizeof(address)) return -1;
    memcpy(out, &address, width);
    return 0;
}
int asp_vendor_load(factory_abi *v) {
    if (!v) return -1;
    memset(v, 0, sizeof(*v));
    library = dlopen("/system/lib/libasp.so", 2);
    if (!library) return -1;
    if (load_one(library, "asp_parameterized_init", &v->init, sizeof(v->init)) ||
        load_one(library, "asp_create_pipeline", &v->create, sizeof(v->create)) ||
        load_one(library, "asp_process", &v->process, sizeof(v->process)) ||
        load_one(library, "asp_destroy_pipeline", &v->destroy, sizeof(v->destroy)) ||
        load_one(library, "asp_deinit", &v->deinit, sizeof(v->deinit))) {
        asp_vendor_unload();
        return -1;
    }
    return 0;
}
void asp_vendor_unload(void) {
    if (library) { (void)dlclose(library); library = NULL; }
}
