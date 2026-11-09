#ifndef CRONOS_ALIAS_POLICY_H
#define CRONOS_ALIAS_POLICY_H
#include <stddef.h>
#define ASP_MAX_MICCAL_ALIASES 64u
int asp_alias_paths(unsigned, char[24], char[80]);
int asp_alias_index(const char *, unsigned *);
int asp_alias_inventory(int, unsigned[ASP_MAX_MICCAL_ALIASES], size_t *);
#endif
