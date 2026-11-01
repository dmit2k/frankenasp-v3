/* Minimal declarations for the pinned Android API-22 softfp build.
 * No host libc headers enter the vendor process. Linux native harness uses
 * identical fixed-width wire fields; only size_t/long follow the target ABI. */
#ifndef ROOK_COMPAT_H
#define ROOK_COMPAT_H
typedef unsigned char u8; typedef unsigned short u16;
typedef unsigned int u32; typedef int i32;
typedef unsigned long long u64; typedef long long i64;
typedef __SIZE_TYPE__ usize; typedef __UINTPTR_TYPE__ uptr;
typedef short i16;
struct Timespec {long sec,nsec;};
struct Poll {int fd;short events,revents;};
extern long read(int,void*,unsigned long),write(int,const void*,unsigned long);
extern int open(const char*,int,...),close(int),poll(struct Poll*,unsigned long,int);
extern int pipe(int*),fork(void),dup2(int,int),execv(const char*,char*const*);
extern int waitpid(int,int*,int),kill(int,int),getpid(void),getrusage(int,void*);
extern void *signal(int,void*);
extern void _exit(int) __attribute__((noreturn));
extern int clock_gettime(int,struct Timespec*),nanosleep(const struct Timespec*,void*);
extern void *malloc(usize),*memcpy(void*,const void*,usize),*memset(void*,int,usize);
extern int memcmp(const void*,const void*,usize),strcmp(const char*,const char*);
extern usize strlen(const char*);
extern int printf(const char*,...),snprintf(char*,usize,const char*,...);
extern double sin(double),cos(double),sqrt(double),log10(double),fabs(double);
extern int atoi(const char*);
static inline u64 ns_now(int clock){struct Timespec t={0,0};clock_gettime(clock,&t);return (u64)t.sec*1000000000ULL+(u64)t.nsec;}
static inline void sleep_ms(int ms){struct Timespec t={ms/1000,(ms%1000)*1000000L};nanosleep(&t,0);}
#endif
