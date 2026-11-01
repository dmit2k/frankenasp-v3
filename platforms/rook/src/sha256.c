#include "core.h"
struct Sha {u32 s[8];u64 bytes;u8 block[64];u32 n;};
static const u32 K[64]={
0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,
0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,
0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,
0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2};
static u32 rr(u32 x,u32 n){return x>>n|x<<(32-n);}
static void transform(struct Sha*c){
 u32 w[64],a,b,d,e,f,g,h,t1,t2,cc;
 for(u32 i=0;i<16;i++)w[i]=(u32)c->block[4*i]<<24|(u32)c->block[4*i+1]<<16|(u32)c->block[4*i+2]<<8|c->block[4*i+3];
 for(u32 i=16;i<64;i++){u32 x=w[i-15],y=w[i-2];w[i]=w[i-16]+(rr(x,7)^rr(x,18)^(x>>3))+w[i-7]+(rr(y,17)^rr(y,19)^(y>>10));}
 a=c->s[0];b=c->s[1];cc=c->s[2];d=c->s[3];e=c->s[4];f=c->s[5];g=c->s[6];h=c->s[7];
 for(u32 i=0;i<64;i++){t1=h+(rr(e,6)^rr(e,11)^rr(e,25))+((e&f)^((~e)&g))+K[i]+w[i];t2=(rr(a,2)^rr(a,13)^rr(a,22))+((a&b)^(a&cc)^(b&cc));h=g;g=f;f=e;e=d+t1;d=cc;cc=b;b=a;a=t1+t2;}
 u32 v[8]={a,b,cc,d,e,f,g,h};for(u32 i=0;i<8;i++)c->s[i]+=v[i];
}
static void init(struct Sha*c){const u32 iv[8]={0x6a09e667,0xbb67ae85,0x3c6ef372,0xa54ff53a,0x510e527f,0x9b05688c,0x1f83d9ab,0x5be0cd19};memset(c,0,sizeof *c);memcpy(c->s,iv,32);}
static void update(struct Sha*c,const u8*p,usize n){c->bytes+=n;while(n--){c->block[c->n++]=*p++;if(c->n==64){transform(c);c->n=0;}}}
static void finish(struct Sha*c,u8*out){u64 bits=c->bytes*8;c->block[c->n++]=128;if(c->n>56){while(c->n<64)c->block[c->n++]=0;transform(c);c->n=0;}while(c->n<56)c->block[c->n++]=0;for(u32 i=0;i<8;i++)c->block[56+i]=(u8)(bits>>(56-8*i));transform(c);for(u32 i=0;i<32;i++)out[i]=(u8)(c->s[i/4]>>(24-8*(i%4)));}
void sha256_bytes(const u8*p,usize n,u8*out){struct Sha c;init(&c);update(&c,p,n);finish(&c,out);}
int file_sha256(const char*path,u8*out){u8 data[8192];struct Sha c;init(&c);int fd=open(path,0);if(fd<0)return -1;long n;while((n=read(fd,data,sizeof data))>0)update(&c,data,(usize)n);close(fd);if(n<0)return -1;finish(&c,out);return 0;}
