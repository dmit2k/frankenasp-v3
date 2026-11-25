typedef __builtin_va_list va_list;
#define va_start(v,l) __builtin_va_start(v,l)
#define va_end(v) __builtin_va_end(v)
extern int vsnprintf(char*,unsigned long,const char*,va_list);
extern long write(int,const void*,unsigned long);
extern unsigned long strlen(const char*);

static void emit(int prio,const char *tag,const char *msg) {
 static const char levels[]="??VDIWEF";
 char head[4]={levels[(prio>=0&&prio<8)?prio:0],'/',0,0};
 write(2,head,2);
 if(tag) write(2,tag,strlen(tag));
 write(2,": ",2);
 if(msg) write(2,msg,strlen(msg));
 write(2,"\n",1);
}

int __android_log_write(int prio,const char *tag,const char *msg) {emit(prio,tag,msg);return 1;}
int __android_log_buf_write(int buf,int prio,const char *tag,const char *msg) {(void)buf;emit(prio,tag,msg);return 1;}
int __android_log_vprint(int prio,const char *tag,const char *fmt,va_list ap) {
 char b[1024];vsnprintf(b,sizeof b,fmt,ap);emit(prio,tag,b);return 1;
}
int __android_log_print(int prio,const char *tag,const char *fmt,...) {
 char b[1024];va_list ap;va_start(ap,fmt);vsnprintf(b,sizeof b,fmt,ap);va_end(ap);emit(prio,tag,b);return 1;
}
int __android_log_buf_print(int buf,int prio,const char *tag,const char *fmt,...) {
 (void)buf;char b[1024];va_list ap;va_start(ap,fmt);vsnprintf(b,sizeof b,fmt,ap);va_end(ap);emit(prio,tag,b);return 1;
}
void __android_log_assert(const char *cond,const char *tag,const char *fmt,...) {
 char b[1024];va_list ap;va_start(ap,fmt);vsnprintf(b,sizeof b,fmt?fmt:"",ap);va_end(ap);emit(7,tag,b);(void)cond;
}
int __android_log_is_loggable(int prio,const char *tag,int def) {(void)prio;(void)tag;(void)def;return 1;}
