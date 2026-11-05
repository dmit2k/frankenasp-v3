/* Proven historical ARM32 Android 5 / API22 __libc_init startup seam. */
#include "bionic_compat.h"
extern int main(int, char **);
static int run(int argc, char **argv, char **env) { (void)env; return main(argc, argv); }
typedef void (*ctor)(void);
static ctor empty[] = { (ctor)-1, 0 };
static struct { ctor *preinit, *init, *fini; } arrays = { empty, empty, empty };
extern _Noreturn void __libc_init(void *, void (*)(void), int (*)(int,char **,char **), const void *);
_Noreturn void entry_c(void *raw) { __libc_init(raw, 0, run, &arrays); }
