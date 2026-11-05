/* Target-only regression for ARM32 Bionic file_sha256() open flags. */
#include "../src/sha256.h"
#ifndef ASP_BIONIC
#error "Run this test with the genuine ARM32 Bionic runtime"
#endif
static const u8 abc_sha[32]={
 0xba,0x78,0x16,0xbf,0x8f,0x01,0xcf,0xea,
 0x41,0x41,0x40,0xde,0x5d,0xae,0x22,0x23,
 0xb0,0x03,0x61,0xa3,0x96,0x17,0x7a,0x9c,
 0xb4,0x10,0xff,0x61,0xf2,0x00,0x15,0xad
};
static void say(const char *s) {(void)write(1,s,strlen(s));}
int main(int argc,char **argv) {
 (void)argc;(void)argv;
 u8 digest[32];
 if(file_sha256("/tmp/rook-sha-regular",digest) ||
    memcmp(digest,abc_sha,sizeof(digest))) {
  say("REGULAR_SHA_FAIL\n");return 1;
 }
 say("REGULAR_SHA_PASS\n");
 errno=0;
 if(file_sha256("/tmp/rook-sha-symlink",digest)!=-1 || errno!=40) {
  say("SYMLINK_ELOOP_FAIL\n");return 2;
 }
 say("SYMLINK_ELOOP_PASS\n");
 return 0;
}
