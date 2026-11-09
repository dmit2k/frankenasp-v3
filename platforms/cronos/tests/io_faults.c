/* Native-only dynamic syscall fault injector. Never linked into production. */
#define _GNU_SOURCE
#include <dlfcn.h>
#include <errno.h>
#include <stddef.h>
#include <string.h>
#include <unistd.h>
static ssize_t (*real_read)(int,void *,size_t);
static ssize_t (*real_write)(int,const void *,size_t);
static unsigned nr,nw;
ssize_t read(int fd, void *buf, size_t size) {
    if (!real_read) {
        void *sym=dlsym(RTLD_NEXT,"read");
        memcpy(&real_read,&sym,sizeof(sym));
    }
    if ((++nr % 19u)==1u) { errno=EINTR; return -1; }
    return real_read(fd,buf,size>7 ? 7 : size);
}
ssize_t write(int fd, const void *buf, size_t size) {
    if (!real_write) {
        void *sym=dlsym(RTLD_NEXT,"write");
        memcpy(&real_write,&sym,sizeof(sym));
    }
    if ((++nw % 23u)==1u) { errno=EINTR; return -1; }
    return real_write(fd,buf,size>11 ? 11 : size);
}
