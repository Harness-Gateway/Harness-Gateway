#define _GNU_SOURCE
#include <dlfcn.h>
#include <netdb.h>
#include <string.h>
int getaddrinfo(const char *name, const char *service,
                const struct addrinfo *hints, struct addrinfo **result) {
    static int (*original)(const char *,const char *,const struct addrinfo *,struct addrinfo **);
    if (!original) original=dlsym(RTLD_NEXT,"getaddrinfo");
    if (name && strcmp(name,"localhost")==0) name="192.0.2.1";
    return original(name,service,hints,result);
}
