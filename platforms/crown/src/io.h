#ifndef ASP1_IO_H
#define ASP1_IO_H
#include "descriptor.h"

int asp_read_exact(int fd, uint8_t *buffer, size_t count);
int asp_write_exact(int fd, const uint8_t *buffer, size_t count);
#endif
