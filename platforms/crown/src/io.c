#define _GNU_SOURCE
#include "io.h"
#ifdef ASP_BIONIC
#include "bionic_compat.h"
#else
#include <errno.h>
#include <unistd.h>
#include <stdlib.h>
#include <string.h>
#include <fcntl.h>
#endif
int asp_read_exact(int fd, uint8_t *p, size_t n) {
    size_t done = 0;
    while (done < n) {
        ssize_t got = read(fd, p + done, n - done);
        if (got < 0 && errno == EINTR) continue;
        if (got < 0) return -1;
        if (!got) return done ? -1 : 0;
        done += (size_t)got;
    }
    return 1;
}
int asp_write_exact(int fd, const uint8_t *p, size_t n) {
    while (n) {
        ssize_t got = write(fd, p, n);
        if (got < 0 && errno == EINTR) continue;
        if (got <= 0) return -1;
        p += (size_t)got;
        n -= (size_t)got;
    }
    return 0;
}
