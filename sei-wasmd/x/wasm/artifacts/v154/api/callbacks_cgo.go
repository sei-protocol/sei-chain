package api

/*
#include "bindings154.h"
#include <stdio.h>

// imports (db)
GoError154 cSet154(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView key, U8SliceView val, UnmanagedVector *errOut);
GoError154 cGet154(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView key, UnmanagedVector *val, UnmanagedVector *errOut);
GoError154 cDelete154(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView key, UnmanagedVector *errOut);
GoError154 cScan154(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView start, U8SliceView end, int32_t order, GoIter *out, UnmanagedVector *errOut);
// imports (iterator)
GoError154 cNext154(iterator_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, UnmanagedVector *key, UnmanagedVector *val, UnmanagedVector *errOut);
GoError154 cNextKey154(iterator_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, UnmanagedVector *key, UnmanagedVector *errOut);
GoError154 cNextValue154(iterator_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, UnmanagedVector *value, UnmanagedVector *errOut);
// imports (api)
GoError154 cHumanAddress154(api_t *ptr, U8SliceView src, UnmanagedVector *dest, UnmanagedVector *errOut, uint64_t *used_gas);
GoError154 cCanonicalAddress154(api_t *ptr, U8SliceView src, UnmanagedVector *dest, UnmanagedVector *errOut, uint64_t *used_gas);
// imports (querier)
GoError154 cQueryExternal154(querier_t *ptr, uint64_t gas_limit, uint64_t *used_gas, U8SliceView request, UnmanagedVector *result, UnmanagedVector *errOut);

// Gateway functions (db)
GoError154 cGet154_cgo(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView key, UnmanagedVector *val, UnmanagedVector *errOut) {
	return cGet154(ptr, gas_meter, used_gas, key, val, errOut);
}
GoError154 cSet154_cgo(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView key, U8SliceView val, UnmanagedVector *errOut) {
	return cSet154(ptr, gas_meter, used_gas, key, val, errOut);
}
GoError154 cDelete154_cgo(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView key, UnmanagedVector *errOut) {
	return cDelete154(ptr, gas_meter, used_gas, key, errOut);
}
GoError154 cScan154_cgo(db_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, U8SliceView start, U8SliceView end, int32_t order, GoIter *out, UnmanagedVector *errOut) {
	return cScan154(ptr, gas_meter, used_gas, start, end, order, out, errOut);
}

// Gateway functions (iterator)
GoError154 cNext154_cgo(iterator_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, UnmanagedVector *key, UnmanagedVector *val, UnmanagedVector *errOut) {
	return cNext154(ptr, gas_meter, used_gas, key, val, errOut);
}
GoError154 cNextKey154_cgo(iterator_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, UnmanagedVector *key, UnmanagedVector *errOut) {
	return cNextKey154(ptr, gas_meter, used_gas, key, errOut);
}
GoError154 cNextValue154_cgo(iterator_t *ptr, gas_meter_t *gas_meter, uint64_t *used_gas, UnmanagedVector *val, UnmanagedVector *errOut) {
	return cNextValue154(ptr, gas_meter, used_gas, val, errOut);
}

// Gateway functions (api)
GoError154 cCanonicalAddress154_cgo(api_t *ptr, U8SliceView src, UnmanagedVector *dest, UnmanagedVector *errOut, uint64_t *used_gas) {
    return cCanonicalAddress154(ptr, src, dest, errOut, used_gas);
}
GoError154 cHumanAddress154_cgo(api_t *ptr, U8SliceView src, UnmanagedVector *dest, UnmanagedVector *errOut, uint64_t *used_gas) {
    return cHumanAddress154(ptr, src, dest, errOut, used_gas);
}

// Gateway functions (querier)
GoError154 cQueryExternal154_cgo(querier_t *ptr, uint64_t gas_limit, uint64_t *used_gas, U8SliceView request, UnmanagedVector *result, UnmanagedVector *errOut) {
    return cQueryExternal154(ptr, gas_limit, used_gas, request, result, errOut);
}
*/
import "C"

// We need these gateway functions to allow calling back to a go function from the c code.
// At least I didn't discover a cleaner way.
// Also, this needs to be in a different file than `callbacks.go`, as we cannot create functions
// in the same file that has //export directives. Only import header types
