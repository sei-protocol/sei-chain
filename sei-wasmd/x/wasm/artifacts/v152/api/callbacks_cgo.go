package api

/*
#include "bindings152.h"
#include <stdio.h>

// imports (db)
GoError152 cSet152(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView key, U8SliceView val, UnmanagedVector *errOut);
GoError152 cGet152(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView key, UnmanagedVector *val, UnmanagedVector *errOut);
GoError152 cDelete152(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView key, UnmanagedVector *errOut);
GoError152 cScan152(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView start, U8SliceView end, int32_t order, GoIter *out, UnmanagedVector *errOut);
// imports (iterator)
GoError152 cNext152(iterator_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, UnmanagedVector *key, UnmanagedVector *val, UnmanagedVector *errOut);
GoError152 cNextKey152(iterator_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, UnmanagedVector *key, UnmanagedVector *errOut);
GoError152 cNextValue152(iterator_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, UnmanagedVector *value, UnmanagedVector *errOut);
// imports (api)
GoError152 cHumanAddress152(api_t *ptr, U8SliceView src, UnmanagedVector *dest, UnmanagedVector *errOut, uint64_t *used_gas);
GoError152 cCanonicalAddress152(api_t *ptr, U8SliceView src, UnmanagedVector *dest, UnmanagedVector *errOut, uint64_t *used_gas);
// imports (querier)
GoError152 cQueryExternal152(querier_t *ptr, uint64_t gas_limit, uint64_t *used_gas, U8SliceView request, UnmanagedVector *result, UnmanagedVector *errOut);

// Gateway functions (db)
GoError152 cGet152_cgo(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView key, UnmanagedVector *val, UnmanagedVector *errOut) {
	return cGet152(ptr, gas_meter, used_gas, key, val, errOut);
}
GoError152 cSet152_cgo(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView key, U8SliceView val, UnmanagedVector *errOut) {
	return cSet152(ptr, gas_meter, used_gas, key, val, errOut);
}
GoError152 cDelete152_cgo(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView key, UnmanagedVector *errOut) {
	return cDelete152(ptr, gas_meter, used_gas, key, errOut);
}
GoError152 cScan152_cgo(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView start, U8SliceView end, int32_t order, GoIter *out, UnmanagedVector *errOut) {
	return cScan152(ptr, gas_meter, used_gas, start, end, order, out, errOut);
}

// Gateway functions (iterator)
GoError152 cNext152_cgo(iterator_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, UnmanagedVector *key, UnmanagedVector *val, UnmanagedVector *errOut) {
	return cNext152(ptr, gas_meter, used_gas, key, val, errOut);
}
GoError152 cNextKey152_cgo(iterator_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, UnmanagedVector *key, UnmanagedVector *errOut) {
	return cNextKey152(ptr, gas_meter, used_gas, key, errOut);
}
GoError152 cNextValue152_cgo(iterator_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, UnmanagedVector *val, UnmanagedVector *errOut) {
	return cNextValue152(ptr, gas_meter, used_gas, val, errOut);
}

// Gateway functions (api)
GoError152 cCanonicalAddress152_cgo(api_t *ptr, U8SliceView src, UnmanagedVector *dest, UnmanagedVector *errOut, uint64_t *used_gas) {
    return cCanonicalAddress152(ptr, src, dest, errOut, used_gas);
}
GoError152 cHumanAddress152_cgo(api_t *ptr, U8SliceView src, UnmanagedVector *dest, UnmanagedVector *errOut, uint64_t *used_gas) {
    return cHumanAddress152(ptr, src, dest, errOut, used_gas);
}

// Gateway functions (querier)
GoError152 cQueryExternal152_cgo(querier_t *ptr, uint64_t gas_limit, uint64_t *used_gas, U8SliceView request, UnmanagedVector *result, UnmanagedVector *errOut) {
    return cQueryExternal152(ptr, gas_limit, used_gas, request, result, errOut);
}
*/
import "C"

// We need these gateway functions to allow calling back to a go function from the c code.
// At least I didn't discover a cleaner way.
// Also, this needs to be in a different file than `callbacks.go`, as we cannot create functions
// in the same file that has //export directives. Only import header types
