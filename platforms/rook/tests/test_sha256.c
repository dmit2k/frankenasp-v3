/* Known-answer and mutation test for the source-pinned library hash gate. */
#include "../src/sha256.h"
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <string.h>
#include <fcntl.h>
static const u8 abc_sha[32] = {
 0xba,0x78,0x16,0xbf,0x8f,0x01,0xcf,0xea,
 0x41,0x41,0x40,0xde,0x5d,0xae,0x22,0x23,
 0xb0,0x03,0x61,0xa3,0x96,0x17,0x7a,0x9c,
 0xb4,0x10,0xff,0x61,0xf2,0x00,0x15,0xad
};
int main(void) {
 char path[]="/tmp/rook-hash-XXXXXX";
 int fd=mkstemp(path);if(fd<0)return 1;
 u8 result[32];int status=0;
 if(write(fd,"abc",3)!=3 || close(fd)<0)status=1;
 if(!status && (file_sha256(path,result) || memcmp(result,abc_sha,32)))status=2;
 fd=open(path,O_WRONLY|O_APPEND);
 if(fd<0 || write(fd,"d",1)!=1 || close(fd)<0)status=3;
 if(!status && (file_sha256(path,result) || !memcmp(result,abc_sha,32)))status=4;
 if(unlink(path)<0)return 5;
 if(!status)puts("ROOK SHA256 PASS: known-answer, mutation, startup identity gate primitive");
 return status;
}
