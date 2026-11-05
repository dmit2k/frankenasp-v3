#ifndef ASP1_BIONIC_COMPAT_H
#define ASP1_BIONIC_COMPAT_H
/* Freestanding ARM32 header seam: only fixed 32-bit Bionic C ABI functions. */
#ifndef ASP_BIONIC
#error "Only for ARM32 Bionic target"
#endif
typedef __INT8_TYPE__ int8_t;
typedef __UINT8_TYPE__ uint8_t;
typedef __INT16_TYPE__ int16_t;
typedef __UINT16_TYPE__ uint16_t;
typedef __INT32_TYPE__ int32_t;
typedef __UINT32_TYPE__ uint32_t;
typedef __INT64_TYPE__ int64_t;
typedef __UINT64_TYPE__ uint64_t;
typedef __SIZE_TYPE__ size_t;
typedef __UINTPTR_TYPE__ uintptr_t;
typedef __PTRDIFF_TYPE__ ssize_t;
typedef int sig_atomic_t;
#define NULL ((void*)0)
#define UINT32_MAX __UINT32_MAX__
#define INT32_MAX __INT32_MAX__
#define INT64_C(x) x##LL
#define UINT32_C(x) x##U
#define UINT64_C(x) x##ULL
#define EINTR 4
#define O_RDONLY 0
#define O_CLOEXEC 02000000
#define O_NOFOLLOW 0100000
_Static_assert(O_RDONLY == 0 && O_CLOEXEC == 02000000 &&
               O_NOFOLLOW == 0100000, "ARM32 open flags");
extern long read(int,void*,unsigned long);
extern long write(int,const void*,unsigned long);
extern int close(int);
extern int dup(int);
extern int dup2(int,int);
extern int open(const char*,int,...);
extern void *malloc(size_t);
extern void free(void*);
extern void *memcpy(void*,const void*,size_t);
extern void *memmove(void*,const void*,size_t);
extern void *memset(void*,int,size_t);
extern int memcmp(const void*,const void*,size_t);
extern size_t strlen(const char*);
extern int strcmp(const char*,const char*);
extern char *getenv(const char*);
extern void *dlopen(const char*,int);
extern void *dlsym(void*,const char*);
typedef struct {const char *dli_fname;void *dli_fbase;const char *dli_sname;void *dli_saddr;} rook_dl_info;
extern int dladdr(const void*,rook_dl_info*);
extern int dlclose(void*);
extern int *__errno(void);
#define errno (*__errno())
extern void _exit(int);
_Static_assert(sizeof(void*)==4, "Bionic helper must be ARM32");
#endif
