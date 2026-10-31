#define _GNU_SOURCE
#include "../launcher/alias_policy.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <assert.h>
#include <unistd.h>
#include <sys/stat.h>
#include <fcntl.h>
int main(void) {
    char template[]="/tmp/asp1-alias-XXXXXX";
    char *base=mkdtemp(template);
    assert(base);
    int dir=open(base,O_RDONLY|O_DIRECTORY);
    assert(dir>=0);
    for(unsigned i=0;i<4;++i) {
        char name[24], target[80], link[96];
        assert(asp_alias_paths(i,name,target)==0);
        assert(symlinkat(target,dir,name)==0);
        ssize_t n=readlinkat(dir,name,link,sizeof(link));
        assert(n>0 && asp_alias_exact(i,link,(size_t)n));
        assert(!asp_alias_exact((i+1)%4,link,(size_t)n));
        assert(!asp_alias_exact(i,"/tmp/spoof",10));
        struct stat st;
        assert(fstatat(dir,name,&st,AT_SYMLINK_NOFOLLOW)==0 && S_ISLNK(st.st_mode));
        assert(unlinkat(dir,name,0)==0);
        assert(readlinkat(dir,name,link,sizeof(link))==-1);
    }
    char name[24],target[80];
    assert(asp_alias_paths(4,name,target)==-1);
    assert(close(dir)==0);
    assert(rmdir(base)==0);
    puts("H02 H08 PASS: compiled production alias policy, four expected targets, spoof/missing rejection");
    return 0;
}
