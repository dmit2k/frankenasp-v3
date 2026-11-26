#include "descriptor.h"
#ifdef ASP_BIONIC
#include "bionic_compat.h"
#else
#include <errno.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#endif

static uint16_t le16(const uint8_t *p) {
 return (uint16_t)((uint16_t)p[0] | (uint16_t)((uint16_t)p[1] << 8));
}
static uint32_t le32(const uint8_t *p) {
 return (uint32_t)p[0] | ((uint32_t)p[1] << 8) | ((uint32_t)p[2] << 16) | ((uint32_t)p[3] << 24);
}
int asp_decode(const uint8_t *wire, size_t length, asp_descriptor *d) {
 if (!wire || !d || length < ASP_HEADER_BYTES || length > ASP_MAX_DESCRIPTOR ||
     memcmp(wire, "ASP1", 4) != 0 || le16(wire + 4) != 1) return -1;
 uint8_t nb = wire[6], nc = wire[7];
 uint32_t size = le32(wire + 8);
 if (!nb || !nc || size != length ||
     length != ASP_HEADER_BYTES + (size_t)nb * ASP_BUFFER_BYTES +
               (size_t)nc * ASP_CHANNEL_BYTES) return -1;
 memset(d, 0, sizeof(*d));
 d->buffer_count = nb; d->channel_count = nc; d->input_bytes = le32(wire + 12);
 uint64_t input_sum = 0;
 unsigned first = 0;
 unsigned mic = 0, ref = 0;
 uint8_t seen_mic[255] = {0}, seen_ref[255] = {0};
 for (unsigned i = 0; i < nb; i++) {
  const uint8_t *p = wire + ASP_HEADER_BYTES + (size_t)i * ASP_BUFFER_BYTES;
  asp_buffer *b = &d->buffers[i];
  b->offset = le32(p); b->length = le32(p + 4); b->sample_rate = le32(p + 8);
  b->frames = le32(p + 12); b->stride = le32(p + 16);
  b->format = p[20]; b->valid_bits = p[21]; b->lsb_shift = p[22];
  b->channels = p[23]; b->first_channel = le16(p + 24);
  unsigned width = b->format == 1 ? 2 : b->format == 2 ? 3 : b->format == 3 ? 4 : 0;
  if (!width || !b->sample_rate || !b->frames || !b->stride || !b->length || !b->channels ||
      le16(p + 26) != 0 || b->offset != input_sum || b->first_channel != first ||
      first + b->channels > nc || b->valid_bits < 2 ||
      b->valid_bits > width * 8 || (unsigned)b->lsb_shift + b->valid_bits > width * 8) return -1;
  for (unsigned j = first; j < first + b->channels; j++) {
   const uint8_t *cp = wire + ASP_HEADER_BYTES + (size_t)nb * ASP_BUFFER_BYTES +
                        (size_t)j * ASP_CHANNEL_BYTES;
   asp_channel *ch = &d->channels[j];
   ch->buffer_index = cp[0]; ch->role = cp[1];
   ch->role_index = le16(cp + 2); ch->frame_offset = le32(cp + 4);
   uint64_t end = (uint64_t)ch->frame_offset + width;
   if (ch->buffer_index != i || end > b->stride ||
       ((uint64_t)b->frames - 1) * b->stride + end > b->length) return -1;
   for (unsigned k = first; k < j; k++) {
    uint64_t prev = d->channels[k].frame_offset;
    if (ch->frame_offset < prev + width && prev < end) return -1;
   }
   if (ch->role_index >= 255) return -1;
   if (ch->role == 1) {
    if (seen_mic[ch->role_index]) return -1;
    seen_mic[ch->role_index] = 1; mic++;
   } else if (ch->role == 2) {
    if (seen_ref[ch->role_index]) return -1;
    seen_ref[ch->role_index] = 1; ref++;
   } else return -1;
  }
  input_sum += b->length;
  if (input_sum > UINT32_MAX) return -1;
  first += b->channels;
 }
 if (first != nc || mic == 0 || input_sum != d->input_bytes) return -1;
 for (unsigned i = 0; i < mic; i++) if (!seen_mic[i]) return -1;
 for (unsigned i = 0; i < ref; i++) if (!seen_ref[i]) return -1;
 return 0;
}
static int read_all(int fd, uint8_t *p, size_t n) {
 while (n) {
  ssize_t got = read(fd, p, n);
  if (got < 0 && errno == EINTR) continue;
  if (got <= 0) return -1;
  p += (size_t)got; n -= (size_t)got;
 }
 return 0;
}
int asp_read_fd(int fd, uint8_t **wire, size_t *length) {
 if (!wire || !length) return -1;
 *wire = NULL; *length = 0;
 uint8_t header[ASP_HEADER_BYTES];
 if (read_all(fd, header, sizeof(header)) != 0) return -1;
 uint32_t total = le32(header + 8);
 if (memcmp(header, "ASP1", 4) != 0 || le16(header + 4) != 1 ||
     !header[6] || !header[7] ||
     total != ASP_HEADER_BYTES + (uint32_t)header[6] * ASP_BUFFER_BYTES +
              (uint32_t)header[7] * ASP_CHANNEL_BYTES) return -1;
 uint8_t *p = malloc(total);
 if (!p) return -1;
 memcpy(p, header, sizeof(header));
 if (read_all(fd, p + ASP_HEADER_BYTES, total - ASP_HEADER_BYTES) != 0) {
  free(p); return -1;
 }
 *wire = p; *length = total;
 return 0;
}
