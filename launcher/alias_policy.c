/* Factory-private four-alias policy. Does not read any calibration bytes. */
#include "alias_policy.h"
#include <stdio.h>
#include <string.h>
int asp_alias_paths(unsigned index, char name[24], char target[80]) {
    if (index >= 4 || !name || !target) return -1;
    int a = snprintf(name, 24, "miccal.%u", index);
    int b = snprintf(target, 80, "/proc/device-tree/idme/miccal.%u/value", index);
    return (a <= 0 || a >= 24 || b <= 0 || b >= 80) ? -1 : 0;
}
int asp_alias_exact(unsigned index, const char *link, size_t length) {
    char name[24], target[80];
    if (!link || asp_alias_paths(index, name, target)) return 0;
    return length == strlen(target) && !memcmp(link, target, length);
}
