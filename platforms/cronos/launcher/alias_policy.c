/* Native aliases only. Never read or interpret calibration coefficients. */
#define _GNU_SOURCE
#include "alias_policy.h"
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

int asp_alias_paths(unsigned index, char name[24], char target[80]) {
    if (index > 255 || !name || !target) return -1;
    int a = snprintf(name, 24, "miccal.%u", index);
    int b = snprintf(target, 80, "/proc/device-tree/idme/miccal.%u/value", index);
    return a <= 0 || a >= 24 || b <= 0 || b >= 80 ? -1 : 0;
}

int asp_alias_index(const char *name, unsigned *index) {
    if (!name || !index || strncmp(name, "miccal.", 7)) return -1;
    const char *p = name + 7;
    if (!*p || (*p == '0' && p[1])) return -1;
    unsigned value = 0;
    for (; *p; ++p) {
        if (*p < '0' || *p > '9' || value > 25) return -1;
        value = value * 10 + (unsigned)(*p - '0');
        if (value > 255) return -1;
    }
    *index = value;
    return 0;
}

int asp_alias_inventory(int fd, unsigned ids[ASP_MAX_MICCAL_ALIASES], size_t *count) {
    if (!ids || !count) return -1;
    *count = 0;
    int duplicate = dup(fd);
    if (duplicate < 0) return -1;
    DIR *dir = fdopendir(duplicate);
    if (!dir) { (void)close(duplicate); return -1; }
    int result = 0;
    errno = 0;
    struct dirent *entry;
    while ((entry = readdir(dir)) != NULL) {
        if (!strcmp(entry->d_name, ".") || !strcmp(entry->d_name, "..")) continue;
        unsigned index;
        char name[24], target[80], actual[80];
        struct stat st;
        if (*count >= ASP_MAX_MICCAL_ALIASES ||
            asp_alias_index(entry->d_name, &index) ||
            asp_alias_paths(index, name, target) ||
            fstatat(fd, name, &st, AT_SYMLINK_NOFOLLOW) ||
            !S_ISLNK(st.st_mode)) { result = -1; break; }
        ssize_t length = readlinkat(fd, name, actual, sizeof(actual));
        if (length < 0 || (size_t)length != strlen(target) ||
            memcmp(actual, target, (size_t)length)) { result = -1; break; }
        ids[(*count)++] = index;
        errno = 0;
    }
    if (errno) result = -1;
    if (closedir(dir)) result = -1;
    for (size_t i = 1; i < *count; ++i) {
        unsigned value = ids[i];
        size_t j = i;
        while (j && ids[j - 1] > value) { ids[j] = ids[j - 1]; --j; }
        ids[j] = value;
    }
    return result;
}
