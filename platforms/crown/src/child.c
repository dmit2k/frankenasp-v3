#include "io.h"
#include "runtime.h"
#include "bionic_compat.h"

static void diagnostic(const char *s) { (void)asp_write_exact(2,(const uint8_t*)s,strlen(s)); }

int main(int argc,char **argv) {
 (void)argv;
 if(argc!=1) {diagnostic("crown: no CLI arguments accepted\n");return 2;}
 int wire=dup(1);
 if(wire<0 || dup2(2,1)<0) {diagnostic("crown: stdout isolation failure\n");return 2;}
 int rc=2,started=0;
 uint8_t *descriptor_bytes=NULL,*input=NULL;
 size_t length=0;
 static asp_descriptor d;
 static asp_runtime runtime;
 factory_abi abi;
 if(asp_read_fd(0,&descriptor_bytes,&length) || asp_decode(descriptor_bytes,length,&d)) {
  diagnostic("crown: invalid PCM-only descriptor\n");goto done;
 }
 if(asp_admit(&d,&runtime)) {diagnostic("crown: unsupported PCM geometry\n");goto done;}
 if(asp_vendor_load(&abi)) {diagnostic("crown: factory library binding failed before READY\n");goto done_vendor;}
 if(asp_runtime_open(&runtime,&abi)) {diagnostic("crown: factory pipeline initialization failed before READY\n");goto done_vendor;}
 started=1;
 input=malloc(d.input_bytes);
 if(!input) {diagnostic("crown: input buffer allocation failed\n");goto done_vendor;}
 {
  uint8_t ready=ASP_READY_BYTE;
  if(asp_write_exact(wire,&ready,1)) goto done_vendor;
 }
 diagnostic("crown: factory ASP ready\n");
 for(;;) {
  int got=asp_read_exact(0,input,d.input_bytes);
  if(!got) {rc=0;break;}
  if(got<0) {diagnostic("crown: truncated input PCM\n");break;}
  uint8_t output[ASP_OUTPUT_BYTES];
  if(asp_step(&runtime,input,output) || asp_write_exact(wire,output,ASP_OUTPUT_BYTES)) {
   diagnostic("crown: vendor process or PCM write failure\n");break;
  }
 }
done_vendor:
 if(started && asp_runtime_close(&runtime)) rc=2;
 asp_vendor_unload();
done:
 free(input);
 free(descriptor_bytes);
 (void)close(wire);
 return rc;
}
