#ifndef ROOK_VENDOR_H
#define ROOK_VENDOR_H
#include "compat.h"
#define ROOK_QUANTUM 128u
#define ROOK_CAPTURE_BYTES (ROOK_QUANTUM * 6u * 3u)
int rook_factory_open(void);
int rook_factory_process(const u8 input[ROOK_CAPTURE_BYTES], const i32 gains[4], i16 output[ROOK_QUANTUM]);
int rook_factory_close(void);
#endif
