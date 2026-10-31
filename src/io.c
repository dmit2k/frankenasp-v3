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
int asp_load_channel_map(unsigned mic[4], unsigned ref[2]) {
    /* Factory-specific captured-slot permutation; never an inferred physical mapping. */
#ifdef ASP_TEST_ONLY
    const char *path = getenv("ASP1_TEST_MAP");
#else
    const char *path = "/factory/channel-map";
#endif
    if (!path) return -1;
    int fd = open(path, O_RDONLY | O_CLOEXEC);
    if (fd < 0) return -1;
    uint8_t buf[96];
    int n = 0;
    while (n < (int)sizeof(buf)) {
        ssize_t got = read(fd, buf + n, sizeof(buf) - (size_t)n);
        if (got < 0 && errno == EINTR) continue;
        if (got <= 0) { if (got < 0) n = -1; break; }
        n += (int)got;
    }
    int closed = close(fd);
    const char prefix[] = "ASP1-CHANNEL-MAP-V1\nmic=";
    if (closed || n != (int)(sizeof(prefix) - 1 + 8 + 4 + 4) ||
        memcmp(buf, prefix, sizeof(prefix) - 1)) return -1;
    unsigned pos = (unsigned)sizeof(prefix) - 1;
    unsigned seen = 0;
    for (unsigned i = 0; i < 4; ++i) {
        uint8_t ch = buf[pos++];
        if (ch < '0' || ch > '3' || (seen & (1u << (ch - '0'))) ||
            buf[pos++] != (i == 3 ? '\n' : ',')) return -1;
        mic[i] = (unsigned)(ch - '0');
        seen |= 1u << (ch - '0');
    }
    if (memcmp(buf + pos, "ref=", 4)) return -1;
    pos += 4; seen = 0;
    for (unsigned i = 0; i < 2; ++i) {
        uint8_t ch = buf[pos++];
        if (ch < '0' || ch > '1' || (seen & (1u << (ch - '0'))) ||
            buf[pos++] != (i == 1 ? '\n' : ',')) return -1;
        ref[i] = (unsigned)(ch - '0');
        seen |= 1u << (ch - '0');
    }
    return 0;
}
