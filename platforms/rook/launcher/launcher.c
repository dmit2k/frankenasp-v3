/* Factory resources and FD-isolated launcher for rook. */
#define _GNU_SOURCE
#define _FILE_OFFSET_BITS 64
#include <elf.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <linux/close_range.h>
#include <sched.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/stat.h>
#include <sys/statvfs.h>
#include <sys/resource.h>
#include <sys/syscall.h>
#include <sys/sysmacros.h>
#include <unistd.h>
#include "alias_policy.h"

#if !defined(__linux__) || !defined(__arm__) || defined(__aarch64__)
#error "Build this launcher for native Linux ARM32, never Bionic or ARM64"
#endif
#ifdef __ANDROID__
#error "The launcher uses Linux libc, not Bionic"
#endif
_Static_assert(sizeof(void *) == 4, "ARM32 required");
_Static_assert(__BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__, "little endian required");

#define DEPLOY_FD 4
#define CHILD "/factory/asp1-child"
#define RO_FLAGS (MS_RDONLY | MS_NOSUID | MS_NODEV)
#define SAFE_FLAGS (MS_NOSUID | MS_NODEV | MS_NOEXEC)

static _Noreturn void fail(const char *step)
{
    int saved = errno;
    dprintf(2, "ASP1 launcher: %s: %s\n", step, strerror(saved));
    _exit(126); /* No fork: all FDs and this private namespace die with us. */
}

static void require(int ok, const char *step)
{
    if (!ok) {
        errno = EPERM;
        fail(step);
    }
}

static struct stat checked(int fd, int directory, int private_dir, int executable)
{
    struct stat st;
    if (fstat(fd, &st) < 0)
        fail("fstat trusted inode");
    require(st.st_uid == 0 && (st.st_mode & 07022) == 0,
            "root ownership/mode");
    require(directory ? S_ISDIR(st.st_mode) : S_ISREG(st.st_mode),
            "inode type");
    if (private_dir)
        require((st.st_mode & 07777) == 0700, "private directory mode");
    if (executable)
        require((st.st_mode & 0500) == 0500, "owner read/execute mode");
    return st;
}

/* Every call opens exactly one literal component from a retained parent FD.
 * The installed root-owned tree must not be updated in place during a session.
 * Root-equivalent mutation is outside this trust model. */
static int component(int parent, const char *name, int directory, int private_dir,
                     int executable)
{
    int flags = O_RDONLY | O_NOFOLLOW | O_CLOEXEC | O_NONBLOCK;
    require(name[0] && !strchr(name, '/') && strcmp(name, ".") &&
            strcmp(name, ".."), "literal component");
    if (directory)
        flags |= O_DIRECTORY;
    int fd = openat(parent, name, flags);
    if (fd < 0)
        fail(name);
    (void)checked(fd, directory, private_dir, executable);
    return fd;
}

static int same_inode(int a, int b)
{
    struct stat x, y;
    if (fstat(a, &x) < 0 || fstat(b, &y) < 0)
        fail("FD identity");
    return x.st_dev == y.st_dev && x.st_ino == y.st_ino;
}

static void read_at(int fd, void *buffer, size_t bytes, off_t offset)
{
    unsigned char *p = buffer;
    while (bytes) {
        ssize_t n = pread(fd, p, bytes, offset);
        if (n < 0 && errno == EINTR)
            continue;
        if (n <= 0) {
            if (!n)
                errno = ENOEXEC;
            fail("read ELF/profile");
        }
        p += n;
        bytes -= (size_t)n;
        offset += n;
    }
}

/* Sanity only for launcher, child, linker and libasp; the original Bionic
 * linker remains responsible for actual DT_NEEDED/symbol resolution. */
static void elf32_arm(int fd, const char *interpreter, int shared)
{
    struct stat st = checked(fd, 0, 0, !shared);
    Elf32_Ehdr h;
    read_at(fd, &h, sizeof h, 0);
    require(!memcmp(h.e_ident, ELFMAG, SELFMAG) &&
            h.e_ident[EI_CLASS] == ELFCLASS32 &&
            h.e_ident[EI_DATA] == ELFDATA2LSB &&
            h.e_ident[EI_VERSION] == EV_CURRENT &&
            h.e_machine == EM_ARM && h.e_version == EV_CURRENT &&
            (h.e_flags & EF_ARM_EABIMASK) == EF_ARM_EABI_VER5 &&
            (h.e_type == ET_EXEC || h.e_type == ET_DYN) &&
            (!shared || h.e_type == ET_DYN) &&
            h.e_ehsize == sizeof h && h.e_phentsize == sizeof(Elf32_Phdr) &&
            h.e_phnum && h.e_phnum <= 128 &&
            (uint64_t)h.e_phoff + (uint64_t)h.e_phnum * sizeof(Elf32_Phdr)
                <= (uint64_t)st.st_size, "ELF32 ARM EABI5 header");
    unsigned int interps = 0, loads = 0;
    for (unsigned int i = 0; i < h.e_phnum; ++i) {
        Elf32_Phdr p;
        read_at(fd, &p, sizeof p, (off_t)h.e_phoff + (off_t)i * sizeof p);
        require((uint64_t)p.p_offset + p.p_filesz <= (uint64_t)st.st_size,
                "ELF segment bounds");
        if (p.p_type == PT_LOAD)
            ++loads;
        if (p.p_type == PT_INTERP) {
            char path[64];
            require(interpreter && ++interps == 1 &&
                    p.p_filesz == strlen(interpreter) + 1 &&
                    p.p_filesz <= sizeof path, "ELF interpreter length");
            read_at(fd, path, p.p_filesz, p.p_offset);
            require(!memcmp(path, interpreter, p.p_filesz),
                    "original Bionic PT_INTERP");
        }
    }
    require(loads && interps == (interpreter ? 1u : 0u),
            "ELF load/interpreter policy");
}

/* Owner-authenticated fixed vendor source pair. Missing or unknown modes fail closed. */
static int profile_mode(int etc) {
    static const char live[] =
        "ASP1-LAUNCHER-V1\nvendor_mode=LIVE_VENDOR_MATCHED\nsource_pair=OWNER_VERIFIED\n";
    static const char bundled[] =
        "ASP1-LAUNCHER-V1\nvendor_mode=OWNER_BUNDLED_MATCHED\nsource_pair=OWNER_VERIFIED\n";
    int fd = component(etc, "launcher.profile", 0, 0, 0);
    struct stat st = checked(fd, 0, 0, 0);
    char text[160];
    require(st.st_size > 0 && (uint64_t)st.st_size < sizeof text, "trusted profile size");
    read_at(fd, text, (size_t)st.st_size, 0);
    int a = st.st_size == (off_t)(sizeof live - 1) &&
        !memcmp(text, live, sizeof live - 1);
    int b = st.st_size == (off_t)(sizeof bundled - 1) &&
        !memcmp(text, bundled, sizeof bundled - 1);
    require(a || b, "owner verified vendor source pair");
    if (close(fd) < 0) fail("close profile");
    return a;
}

static void link_is(int parent, const char *name, const char *destination)
{
    char buffer[160];
    struct stat st;
    if (fstatat(parent, name, &st, AT_SYMLINK_NOFOLLOW) < 0)
        fail("proc alias stat");
    require(S_ISLNK(st.st_mode) && st.st_uid == 0, "root-owned proc alias");
    ssize_t n = readlinkat(parent, name, buffer, sizeof buffer);
    if (n < 0)
        fail("proc alias readlink");
    require((size_t)n == strlen(destination) &&
            !memcmp(buffer, destination, (size_t)n), "exact proc alias");
}

static void proc_layout(int root)
{
    int proc = component(root, "proc", 1, 0, 0);
    link_is(proc, "self", "/.realproc/self");
    link_is(proc, "sys", "/.realproc/sys");
    link_is(proc, "cpuinfo", "/.realproc/cpuinfo");
    link_is(proc, "device-tree", "/sys/firmware/devicetree/base");
    int idme = component(proc, "idme", 1, 0, 0);
    for (int i = 0; i < 4; ++i) {
        char name[24], target[80];
        require(asp_alias_paths((unsigned)i, name, target) == 0, "fixed miccal alias policy");
        link_is(idme, name, target);
    }
    if (close(idme) < 0 || close(proc) < 0)
        fail("close proc layout");
}

static void fd_path(int fd, char buffer[64])
{
    snprintf(buffer, 64, "/proc/self/fd/%d", fd);
}

static void bind_ro(int source, const char *target)
{
    char path[64];
    fd_path(source, path);
    if (mount(path, target, NULL, MS_BIND, NULL) < 0 ||
        mount(NULL, target, NULL, MS_BIND | MS_REMOUNT | RO_FLAGS, NULL) < 0)
        fail("read-only bind");
}

static void mount_fs(const char *type, const char *target,
                     unsigned long flags, const char *options)
{
    if (mount(type, target, type, flags, options) < 0)
        fail(target);
}

static void factory_sanity(int vendor, int system)
{
    int lib = component(system, "lib", 1, 0, 0);
    int asp = component(lib, "libasp.so", 0, 0, 0);
    elf32_arm(asp, NULL, 1);
    int etc = component(vendor, "etc", 1, 0, 0);
    int configs = component(etc, "audio-algorithms", 1, 0, 0);
    const char *names[] = {"AFE.cfg", "coefs_FBF.cfg"};
    for (size_t i = 0; i < sizeof names / sizeof names[0]; ++i) {
        int fd = component(configs, names[i], 0, 0, 0);
        struct stat st = checked(fd, 0, 0, 0);
        require(st.st_size > 0, "nonempty vendor config");
        if (close(fd) < 0)
            fail("close vendor config");
    }
    if (close(configs) < 0 || close(etc) < 0 || close(asp) < 0 || close(lib) < 0)
        fail("close vendor sanity");
}

int main(int argc, char **argv)
{
    (void)argv;
    require(argc == 1 && getuid() == 0 && geteuid() == 0, "root/no CLI");
    umask(077);
    (void)checked(3, 0, 0, 1);
    (void)checked(DEPLOY_FD, 1, 1, 0);
    int self = open("/proc/self/exe", O_RDONLY | O_CLOEXEC);
    if (self < 0)
        fail("running launcher identity");
    require(same_inode(3, self), "FD3 is this launcher");
    elf32_arm(3, NULL, 0); /* Static Linux launcher, no PT_INTERP. */
    if (close(self) < 0)
        fail("close launcher identity");

    int lib = component(DEPLOY_FD, "lib", 1, 1, 0);
    int root = component(lib, "rootfs", 1, 1, 0);
    int etc = component(DEPLOY_FD, "etc", 1, 1, 0);
    int live = profile_mode(etc);
    require(!live, "rook requires matched bundled system/vendor");
    proc_layout(root);

    int system = component(root, "system", 1, 0, 0);
    int bin = component(system, "bin", 1, 0, 0);
    int linker = component(bin, "linker", 0, 0, 1);
    elf32_arm(linker, NULL, 0);
    int factory = component(root, "factory", 1, 0, 0);
    int child = component(factory, "asp1-child", 0, 0, 1);
    elf32_arm(child, "/system/bin/linker", 0);

    int vendor;
    if (live) {
        int host_root = open("/", O_RDONLY | O_DIRECTORY | O_CLOEXEC);
        if (host_root < 0)
            fail("host root");
        (void)checked(host_root, 1, 0, 0);
        vendor = component(host_root, "vendor", 1, 0, 0);
        struct statvfs vfs;
        if (fstatvfs(vendor, &vfs) < 0)
            fail("live vendor filesystem");
        require(vfs.f_flag & ST_RDONLY, "original live vendor must already be RO");
        if (close(host_root) < 0)
            fail("close host root");
    } else {
        vendor = component(root, "vendor", 1, 0, 0);
    }
    factory_sanity(vendor, system);
    const char *mountpoints[] = {"vendor", "sys", ".realproc", "dev", "data"};
    for (size_t i = 0; i < sizeof mountpoints / sizeof mountpoints[0]; ++i) {
        int fd = component(root, mountpoints[i], 1, 0, 0);
        if (close(fd) < 0)
            fail("close mountpoint");
    }

    if (unshare(CLONE_NEWNS) < 0)
        fail("private mount namespace");
    /* Absolutely no mounts before this barrier after unshare. */
    if (mount(NULL, "/", NULL, MS_REC | MS_PRIVATE, NULL) < 0)
        fail("recursive private propagation");
    /* FDs opened before unshare still refer to old-namespace mount objects.
     * Reopen from the new namespace and compare inodes with the pinned FDs
     * before using these mount sources or targets. */
    int ns_deploy = open("/data/techo5-linux/external-asp",
                         O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    if (ns_deploy < 0)
        fail("open deployment in new mount namespace");
    (void)checked(ns_deploy, 1, 1, 0);
    require(same_inode(DEPLOY_FD, ns_deploy), "deployment namespace identity");
    int ns_lib = component(ns_deploy, "lib", 1, 1, 0);
    require(same_inode(lib, ns_lib), "lib namespace identity");
    int ns_root = component(ns_lib, "rootfs", 1, 1, 0);
    require(same_inode(root, ns_root), "rootfs namespace identity");
    if (fchdir(ns_root) < 0)
        fail("new namespace rootfs cwd");
    char path[64];
    fd_path(ns_root, path);
    if (mount(path, ".", NULL, MS_BIND, NULL) < 0)
        fail("single installed rootfs bind");
    /* Re-enter the new bind mount; an old-namespace FD bypasses its
     * read-only remount and is not a valid source for later binds. */
    int mounted_root = component(ns_lib, "rootfs", 1, 1, 0);
    require(same_inode(root, mounted_root), "mounted rootfs identity");
    if (fchdir(mounted_root) < 0)
        fail("mounted rootfs cwd");

    int ns_system = component(mounted_root, "system", 1, 0, 0);
    require(same_inode(system, ns_system), "system namespace identity");
    int ns_vendor;
    if (live) {
        ns_vendor = open("/vendor", O_RDONLY | O_DIRECTORY |
                        O_NOFOLLOW | O_CLOEXEC);
        if (ns_vendor < 0)
            fail("open live vendor in new mount namespace");
        (void)checked(ns_vendor, 1, 0, 0);
    } else {
        ns_vendor = component(mounted_root, "vendor", 1, 0, 0);
    }
    require(same_inode(vendor, ns_vendor), "vendor namespace identity");
    bind_ro(ns_system, "system");
    bind_ro(ns_vendor, "vendor");
    mount_fs("sysfs", "sys", RO_FLAGS | MS_NOEXEC, NULL);
    mount_fs("proc", ".realproc", MS_RDONLY | SAFE_FLAGS, NULL);
    mount_fs("tmpfs", "dev", MS_NOSUID | MS_NOEXEC, "size=256k,mode=0755");
    const char *devices[] = {"dev/null", "dev/zero", "dev/random", "dev/urandom"};
    const unsigned int minors[] = {3, 5, 8, 9};
    for (size_t i = 0; i < sizeof devices / sizeof devices[0]; ++i) {
        if (mknod(devices[i], S_IFCHR | 0600, makedev(1, minors[i])) < 0)
            fail("private device node");
    }
    if (symlink("/proc/self/fd", "dev/fd") < 0 ||
        symlink("/proc/self/fd/0", "dev/stdin") < 0 ||
        symlink("/proc/self/fd/1", "dev/stdout") < 0 ||
        symlink("/proc/self/fd/2", "dev/stderr") < 0)
        fail("private dev aliases");
    mount_fs("tmpfs", "data", SAFE_FLAGS, "size=16m,mode=0700");
    if (mount(NULL, ".", NULL, MS_BIND | MS_REMOUNT | RO_FLAGS, NULL) < 0)
        fail("read-only installed rootfs");

    if (chroot(".") < 0 || chdir("/") < 0)
        fail("chroot pinned rootfs");
    /* Verify aliases reach authentic readable sysfs nodes, without reading,
     * parsing, copying or logging even one calibration byte. */
    for (int i = 0; i < 4; ++i) {
        char miccal[40];
        snprintf(miccal, sizeof miccal, "/proc/idme/miccal.%d", i);
        int fd = open(miccal, O_RDONLY | O_CLOEXEC | O_NONBLOCK);
        if (fd < 0)
            fail("authentic miccal path unavailable");
        struct stat st;
        if (fstat(fd, &st) < 0)
            fail("miccal path stat");
        require(S_ISREG(st.st_mode), "miccal sysfs type");
        if (close(fd) < 0)
            fail("close miccal path");
    }
    sigset_t empty;
    if (sigemptyset(&empty) < 0 || sigprocmask(SIG_SETMASK, &empty, NULL) < 0 ||
        signal(SIGPIPE, SIG_DFL) == SIG_ERR ||
        signal(SIGTERM, SIG_DFL) == SIG_ERR || signal(SIGINT, SIG_DFL) == SIG_ERR)
        fail("child signals");
    /* Preserve FD-pinned exec on kernels predating close_range (rook 4.9).
     * Always mark every inherited descriptor CLOEXEC, including FD3/FD4. */
    if (syscall(SYS_close_range, 3u, UINT_MAX, CLOSE_RANGE_CLOEXEC) < 0) {
        if (errno != ENOSYS) fail("close_range CLOEXEC");
        struct rlimit limit;
        if (getrlimit(RLIMIT_NOFILE, &limit) < 0 ||
            limit.rlim_cur > 65536) fail("bounded FD audit");
        for (unsigned int fd = 3; fd < (unsigned int)limit.rlim_cur; ++fd)
            if (fcntl((int)fd, F_SETFD, FD_CLOEXEC) < 0 && errno != EBADF)
                fail("fallback FD CLOEXEC");
    }
    char *const child_argv[] = {CHILD, NULL};
    char *const child_env[] = {
        "LANG=C", "LC_ALL=C", "PATH=/system/bin",
        "LD_LIBRARY_PATH=/system/lib:/vendor/lib",
        NULL
    };
    execveat(child, "", child_argv, child_env, AT_EMPTY_PATH);
    fail("exec original ARM32 Bionic child");
}
