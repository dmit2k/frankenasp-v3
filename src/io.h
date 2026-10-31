#ifndef ASP1_IO_H
#define ASP1_IO_H
#include "descriptor.h"
/* read returns 1 full, 0 clean EOF before block, -1 partial/error. */
int asp_read_exact(int fd, uint8_t *buffer, size_t count);
int asp_write_exact(int fd, const uint8_t *buffer, size_t count);
int asp_load_channel_map(unsigned mic[4], unsigned ref[2]);
#endif
