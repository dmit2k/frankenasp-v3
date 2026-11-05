#ifndef ROOK_SHA256_H
#define ROOK_SHA256_H
#ifdef ASP_BIONIC
#include "bionic_compat.h"
#else
#include <stddef.h>
#include <stdint.h>
#include <string.h>
#include <unistd.h>
#include <fcntl.h>
#include <errno.h>
#endif
typedef uint8_t u8;typedef uint32_t u32;typedef uint64_t u64;typedef size_t usize;
int file_sha256(const char*,u8[32]);
#endif
