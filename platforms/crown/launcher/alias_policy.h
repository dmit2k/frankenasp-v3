#ifndef ASP1_ALIAS_POLICY_H
#define ASP1_ALIAS_POLICY_H
#include <stddef.h>
int asp_alias_paths(unsigned index, char name[24], char target[80]);
int asp_alias_exact(unsigned index, const char *link, size_t length);
#endif
