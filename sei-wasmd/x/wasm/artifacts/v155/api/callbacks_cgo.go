package api

/*
#include "bindings155.h"
#include <stdio.h>

// imports (db)
GoError155 cSet155(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView key, U8SliceView val, UnmanagedVector *errOut);
GoError155 cGet155(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView key, UnmanagedVector *val, UnmanagedVector *errOut);
GoError155 cDelete155(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView key, UnmanagedVector *errOut);
GoError155 cScan155(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView start, U8SliceView end, int32_t order, GoIter *out, UnmanagedVector *errOut);
// imports (iterator)
GoError155 cNext155(iterator_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, UnmanagedVector *key, UnmanagedVector *val, UnmanagedVector *errOut);
GoError155 cNextKey155(iterator_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, UnmanagedVector *key, UnmanagedVector *errOut);
GoError155 cNextValue155(iterator_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, UnmanagedVector *value, UnmanagedVector *errOut);
// imports (api)
GoError155 cHumanAddress155(api_t *ptr, U8SliceView src, UnmanagedVector *dest, UnmanagedVector *errOut, uint64_t *used_gas);
GoError155 cCanonicalAddress155(api_t *ptr, U8SliceView src, UnmanagedVector *dest, UnmanagedVector *errOut, uint64_t *used_gas);
// imports (querier)
GoError155 cQueryExternal155(querier_t *ptr, uint64_t gas_limit, uint64_t *used_gas, U8SliceView request, UnmanagedVector *result, UnmanagedVector *errOut);

// Gateway functions (db)
GoError155 cGet155_cgo(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView key, UnmanagedVector *val, UnmanagedVector *errOut) {
	return cGet155(ptr, gas_meter, used_gas, key, val, errOut);
}
GoError155 cSet155_cgo(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView key, U8SliceView val, UnmanagedVector *errOut) {
	return cSet155(ptr, gas_meter, used_gas, key, val, errOut);
}
GoError155 cDelete155_cgo(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView key, UnmanagedVector *errOut) {
	return cDelete155(ptr, gas_meter, used_gas, key, errOut);
}
GoError155 cScan155_cgo(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView start, U8SliceView end, int32_t order, GoIter *out, UnmanagedVector *errOut) {
	return cScan155(ptr, gas_meter, used_gas, start, end, order, out, errOut);
}

// Gateway functions (iterator)
GoError155 cNext155_cgo(iterator_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, UnmanagedVector *key, UnmanagedVector *val, UnmanagedVector *errOut) {
	return cNext155(ptr, gas_meter, used_gas, key, val, errOut);
}
GoError155 cNextKey155_cgo(iterator_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, UnmanagedVector *key, UnmanagedVector *errOut) {
	return cNextKey155(ptr, gas_meter, used_gas, key, errOut);
}
GoError155 cNextValue155_cgo(iterator_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, UnmanagedVector *val, UnmanagedVector *errOut) {
	return cNextValue155(ptr, gas_meter, used_gas, val, errOut);
}

// Gateway functions (api)
GoError155 cCanonicalAddress155_cgo(api_t *ptr, U8SliceView src, UnmanagedVector *dest, UnmanagedVector *errOut, uint64_t *used_gas) {
    return cCanonicalAddress155(ptr, src, dest, errOut, used_gas);
}
GoError155 cHumanAddress155_cgo(api_t *ptr, U8SliceView src, UnmanagedVector *dest, UnmanagedVector *errOut, uint64_t *used_gas) {
    return cHumanAddress155(ptr, src, dest, errOut, used_gas);
}

// Gateway functions (querier)
GoError155 cQueryExternal155_cgo(querier_t *ptr, uint64_t gas_limit, uint64_t *used_gas, U8SliceView request, UnmanagedVector *result, UnmanagedVector *errOut) {
    return cQueryExternal155(ptr, gas_limit, used_gas, request, result, errOut);
}
*/
import "C"

// We need these gateway functions to allow calling back to a go function from the c code.
// At least I didn't discover a cleaner way.
// Also, this needs to be in a different file than `callbacks.go`, as we cannot create functions
// in the same file that has //export directives. Only import header types
