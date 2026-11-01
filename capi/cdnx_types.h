#ifndef CDNX_TYPES_H
#define CDNX_TYPES_H
#include <stdint.h>
#include <stddef.h>

typedef struct {
    double value;
    double lower;
    double upper;
    int32_t empty;
} cdnx_estimate_t;

enum {
    CDNX_OK = 0,
    CDNX_EINVAL = 1,
    CDNX_ENOTFOUND = 2,
    CDNX_EINCOMPATIBLE = 3,
    CDNX_ECORRUPT = 4,
    CDNX_EKIND = 5,
    CDNX_EINTERNAL = 6
};
#endif
