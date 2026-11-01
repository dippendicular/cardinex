#include <string.h>
#include "cdnx_types.h"

#if defined(_MSC_VER)
#define CDNX_TLS __declspec(thread)
#else
#define CDNX_TLS _Thread_local
#endif

static CDNX_TLS char cdnx_err[1024];

void cdnx_set_err(const char *s) {
    strncpy(cdnx_err, s, sizeof cdnx_err - 1);
    cdnx_err[sizeof cdnx_err - 1] = 0;
}

int32_t cdnx_last_error(char *buf, size_t len) {
    size_t n = strlen(cdnx_err);
    if (buf && len > 0) {
        size_t c = n < len - 1 ? n : len - 1;
        memcpy(buf, cdnx_err, c);
        buf[c] = 0;
    }
    return (int32_t)n;
}
