#ifndef FRANKENASP_V3_DESCRIPTOR_H
#define FRANKENASP_V3_DESCRIPTOR_H
#include <stddef.h>
#include <stdint.h>
#define ASP_HEADER_BYTES 16u
#define ASP_BUFFER_BYTES 28u
#define ASP_CHANNEL_BYTES 8u
#define ASP_MAX_DESCRIPTOR (ASP_HEADER_BYTES + 255u * ASP_BUFFER_BYTES + 255u * ASP_CHANNEL_BYTES)
#define ASP_READY_BYTE 0xa5u
#define ASP_OUTPUT_BYTES 640u
typedef struct {
 uint32_t offset, length, sample_rate, frames, stride;
 uint8_t format, valid_bits, lsb_shift, channels;
 uint16_t first_channel;
} asp_buffer;
typedef struct {
 uint8_t buffer_index, role;
 uint16_t role_index;
 uint32_t frame_offset;
} asp_channel;
typedef struct {
 uint8_t buffer_count, channel_count;
 uint32_t input_bytes;
 asp_buffer buffers[255];
 asp_channel channels[255];
} asp_descriptor;
/* Return 0 on success, -1 on malformed/truncated input. */
int asp_decode(const uint8_t *wire, size_t length, asp_descriptor *desc);
/* Read precisely one bounded startup descriptor from fd; does not consume PCM.
   Caller owns *wire and must free it. */
int asp_read_fd(int fd, uint8_t **wire, size_t *length);
#endif
