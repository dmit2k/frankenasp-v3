#define _GNU_SOURCE
#include "../launcher/alias_policy.h"
#include <assert.h>
#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>
int main(int argc, char **argv) {
    char name[24], target[80];
    unsigned index;
    assert(asp_alias_paths(255, name, target) == 0);
    assert(!strcmp(name, "miccal.255"));
    assert(asp_alias_paths(256, name, target) != 0);
    const char *bad[] = {"", "miccal.", "miccal.01", "miccal.-1", "miccal.256",
                         "miccal.999999999999999", "../miccal.0", "miccal.1/value"};
    for (unsigned i = 0; i < sizeof(bad)/sizeof(bad[0]); ++i)
        assert(asp_alias_index(bad[i], &index) != 0);
    assert(asp_alias_index("miccal.0", &index) == 0 && index == 0);
    if (argc == 1) { puts("CRONOS ALIAS INDEX PASS"); return 0; }
    assert(argc == 2);
    int fd = open(argv[1], O_RDONLY | O_DIRECTORY | O_NOFOLLOW);
    if (fd < 0) return 2;
    unsigned ids[ASP_MAX_MICCAL_ALIASES];
    size_t count;
    int rc = asp_alias_inventory(fd, ids, &count);
    (void)close(fd);
    if (rc) return 2;
    printf("%zu", count);
    for (size_t i = 0; i < count; ++i) printf(" %u", ids[i]);
    puts("");
    return 0;
}
