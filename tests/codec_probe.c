#include "../src/descriptor.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
int main(int argc, char **argv) {
 if (argc == 2 && strcmp(argv[1], "--ready") == 0) {
  printf("%02X\n", ASP_READY_BYTE); return 0;
 }
 int sample = argc == 2 && strcmp(argv[1], "--sample") == 0;
 int roundtrip = argc == 2 && strcmp(argv[1], "--roundtrip") == 0;
 int unaligned = argc == 2 && strcmp(argv[1], "--unaligned") == 0;
 int strict = argc == 2 && strcmp(argv[1], "--strict") == 0;
 if (!(argc == 1 || sample || roundtrip || unaligned || strict)) return 2;
 uint8_t *wire = NULL; size_t n = 0;
 if (asp_read_fd(0, &wire, &n) != 0) return 2;
 if (unaligned) {
  uint8_t *base = malloc(n + 1);
  if (!base) { free(wire); return 2; }
  memcpy(base + 1, wire, n);
  free(wire); wire = base;
 }
 asp_descriptor *d = malloc(sizeof(*d));
 if (!d) { free(wire); return 2; }
 if (asp_decode(wire + (unaligned ? 1 : 0), n, d) != 0 ||
     (strict && getchar() != EOF)) { free(d); free(wire); return 2; }
 if (sample || roundtrip) {
  if (d->input_bytes > (1u << 20)) { free(d); free(wire); return 2; }
  uint8_t *pcm = malloc(d->input_bytes);
  if (!pcm) { free(d); free(wire); return 2; }
  if (fread(pcm, 1, d->input_bytes, stdin) != d->input_bytes) {
   free(pcm); free(d); free(wire); return 2;
  }
  if (roundtrip) {
   int status = fwrite(pcm, 1, d->input_bytes, stdout) == d->input_bytes ? 0 : 2;
   free(pcm); free(d); free(wire); return status;
  }
  for (unsigned i=0;i<d->channel_count;i++) {
   const asp_channel *ch=&d->channels[i];
   const asp_buffer *b=&d->buffers[ch->buffer_index];
   const uint8_t *v=pcm+b->offset+ch->frame_offset;
   unsigned width=b->format==1?2:b->format==2?3:4;
   printf("S %u %u ",ch->role,ch->role_index);
   for (unsigned k=0;k<width;k++) printf("%02x",v[k]);
   printf("\n");
  }
  free(pcm); free(d); free(wire); return 0;
 }
 printf("D %zu %u %u %u\n",n,d->input_bytes,d->buffer_count,d->channel_count);
 for (unsigned i=0;i<d->buffer_count;i++) {
  const asp_buffer *b=&d->buffers[i];
  printf("B %u %u %u %u %u %u %u %u %u %u %u\n",b->offset,b->length,b->sample_rate,b->frames,b->stride,b->format,b->valid_bits,b->lsb_shift,b->channels,b->first_channel,i);
 }
 for (unsigned i=0;i<d->channel_count;i++) {
  const asp_channel *c=&d->channels[i];
  printf("C %u %u %u %u\n",c->buffer_index,c->role,c->role_index,c->frame_offset);
 }
 free(d);free(wire);return 0;
}
