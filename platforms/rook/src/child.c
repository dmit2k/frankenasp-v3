/* PCM-only ASP1 child: private factory runtime, no Android service registration. */
#include "vendor.h"
#include "descriptor.h"
#include "io.h"
#include "bionic_compat.h"

#define BLOCK_FRAMES 320u
#define BLOCK_BYTES (BLOCK_FRAMES * 18u)
#define OUTPUT_BYTES 640u
static void diagnostic(const char *message) {
    (void)asp_write_exact(2, (const uint8_t *)message, strlen(message));
}
static int calibration(i32 gains[4]) {
    for (unsigned i = 0; i < 4; ++i) {
        char path[] = "/proc/idme/miccal.0";
        path[sizeof(path) - 2] = (char)('0' + i);
        int fd = open(path, O_RDONLY | O_CLOEXEC);
        if (fd < 0) return -1;
        char data[32];
        long n = read(fd, data, sizeof(data));
        int closed = close(fd);
        if (closed || n < 1 || n >= (long)sizeof(data)) return -1;
        usize at = 0;
        i64 v = 0;
        while (at < (usize)n && data[at] >= '0' && data[at] <= '9') {
            v = v * 10 + data[at++] - '0';
            if (v > 2147483647LL) return -1;
        }
        if (!at) return -1;
        while (at < (usize)n && (data[at] == 0 || data[at] == '\n')) ++at;
        if (at != (usize)n || v <= 0) return -1;
        if (((u32)v & 0x7fff0000U) == 0) v <<= 16;
        if (v <= 0 || v > 2147483647LL) return -1;
        gains[i] = (i32)v;
    }
    return 0;
}
static int admitted(const asp_descriptor *d) {
    if (d->buffer_count != 1 || d->channel_count != 6 ||
        d->input_bytes != BLOCK_BYTES) return -1;
    const asp_buffer *b = &d->buffers[0];
    if (b->offset || b->length != BLOCK_BYTES || b->sample_rate != 16000 ||
        b->frames != BLOCK_FRAMES || b->stride != 18 ||
        b->format != 2 || b->valid_bits != 24 || b->lsb_shift ||
        b->channels != 6 || b->first_channel) return -1;
    for (unsigned i = 0; i < 6; ++i) {
        const asp_channel *c = &d->channels[i];
        if (c->buffer_index || c->frame_offset != 3 * i ||
            c->role != (i < 4 ? 1 : 2) ||
            c->role_index != (i < 4 ? i : i - 4)) return -1;
    }
    return 0;
}
int main(int argc, char **argv) {
    (void)argv;
    if (argc != 1) return 2;
    int wire = dup(1);
    if (wire < 0 || dup2(2, 1) < 0) return 2;
    uint8_t *encoded = 0;
    usize encoded_bytes = 0;
    asp_descriptor desc;
    i32 gains[4];
    int rc = 2;
    int started = 0;
    if (asp_read_fd(0, &encoded, &encoded_bytes) ||
        asp_decode(encoded, encoded_bytes, &desc) ||
        admitted(&desc) || calibration(gains)) {
        diagnostic("rook: invalid descriptor or unavailable authentic calibration\n");
        goto done;
    }
    if (rook_factory_open()) {
        diagnostic("rook: factory resources or ABI rejected\n");
        goto done;
    }
    started = 1;
    if (asp_write_exact(wire, (const uint8_t[]){ASP_READY_BYTE}, 1)) goto done;
    uint8_t input[BLOCK_BYTES];
    uint8_t quantum[ROOK_CAPTURE_BYTES];
    i16 output_fifo[640] = {0};
    i16 result[ROOK_QUANTUM];
    unsigned pending = 0, available = 64; /* Exactly one 4-ms FIFO pad. */
    for (;;) {
        int got = asp_read_exact(0, input, BLOCK_BYTES);
        if (got == 0) { rc = 0; break; }
        if (got < 0) { diagnostic("rook: truncated PCM\n"); break; }
        /* A full native zero block is the privacy software-cut representation.
         * Advance factory state but never publish a delayed pre-mute sample. */
        unsigned silent = 1;
        for (unsigned i = 0; i < BLOCK_BYTES; ++i) silent &= input[i] == 0;
        for (unsigned frame = 0; frame < BLOCK_FRAMES; ++frame) {
            memcpy(quantum + pending * 18, input + frame * 18, 18);
            if (++pending == ROOK_QUANTUM) {
                if (available > 512 ||
                    rook_factory_process(quantum, gains, result)) {
                    diagnostic("rook: factory DSP processing failed\n");
                    goto done;
                }
                memcpy(output_fifo + available, result, sizeof(result));
                available += ROOK_QUANTUM;
                pending = 0;
            }
        }
        if (available < BLOCK_FRAMES) goto done;
        uint8_t packet[OUTPUT_BYTES];
        for (unsigned i = 0; i < BLOCK_FRAMES; ++i) {
            u16 value = (u16)output_fifo[i];
            packet[2*i] = (u8)value;
            packet[2*i+1] = (u8)(value >> 8);
        }
        if (silent) memset(packet, 0, sizeof(packet));
        if (asp_write_exact(wire, packet, OUTPUT_BYTES)) goto done;
        available -= BLOCK_FRAMES;
        memmove(output_fifo, output_fifo + BLOCK_FRAMES, available * sizeof(i16));
    }
done:
    if (started && rook_factory_close()) rc = 2;
    if (encoded) free(encoded);
    close(wire);
    return rc;
}
