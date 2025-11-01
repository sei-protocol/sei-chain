package types

import (
	"encoding/hex"
	"encoding/json"
	"sync"
)

type StoreTracer struct {
	Modules map[string]*ModuleTrace
	mu      *sync.Mutex
}

type ModuleTrace struct {
	Accesses []Access
}

type Access struct {
	Op    OpType
	Key   []byte
	Value []byte
}

type OpType int

const (
	Get OpType = iota
	Has
	Set
	Delete
)

func NewStoreTracer() *StoreTracer {
	return &StoreTracer{
		Modules: map[string]*ModuleTrace{},
		mu:      &sync.Mutex{},
	}
}

func (st *StoreTracer) Get(key []byte, value []byte, module string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	accesses := st.getOrSetModuleTrace(module)
	accesses.Accesses = append(accesses.Accesses, Access{
		Op:    Get,
		Key:   key,
		Value: value,
	})
}

func (st *StoreTracer) Set(key []byte, value []byte, module string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	accesses := st.getOrSetModuleTrace(module)
	accesses.Accesses = append(accesses.Accesses, Access{
		Op:    Set,
		Key:   key,
		Value: value,
	})
}

func (st *StoreTracer) Has(key []byte, module string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	accesses := st.getOrSetModuleTrace(module)
	accesses.Accesses = append(accesses.Accesses, Access{
		Op:  Has,
		Key: key,
	})
}

func (st *StoreTracer) Delete(key []byte, module string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	accesses := st.getOrSetModuleTrace(module)
	accesses.Accesses = append(accesses.Accesses, Access{
		Op:  Delete,
		Key: key,
	})
}

func (st *StoreTracer) getOrSetModuleTrace(module string) (mt *ModuleTrace) {
	if _, ok := st.Modules[module]; !ok {
		mt = &ModuleTrace{
			Accesses: []Access{},
		}
		st.Modules[module] = mt
	} else {
		mt = st.Modules[module]
	}
	return
}

func (st *StoreTracer) Clear() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.Modules = map[string]*ModuleTrace{}
}

type StoreTraceDump struct {
	Modules map[string]ModuleTraceDump `json:"modules"`
}

type ModuleTraceDump struct {
	Reads map[string]string `json:"reads"`
	Has   []string          `json:"has"`
}

func (st *StoreTracer) DerivePrestateToJson() []byte {
	st.mu.Lock()
	defer st.mu.Unlock()
	d := StoreTraceDump{
		Modules: make(map[string]ModuleTraceDump, len(st.Modules)),
	}
	for name, module := range st.Modules {
		mtd := ModuleTraceDump{
			Reads: make(map[string]string),
			Has:   []string{},
		}
		// any read for key XYZ after a Set/Delete to XYZ is discarded
		// because the result doesn't represent prestate.
		writtenKey := map[string]struct{}{}
		hasMap := map[string]struct{}{}
		for _, a := range module.Accesses {
			switch a.Op {
			case Get:
				if _, ok := writtenKey[string(a.Key)]; ok {
					continue
				}
				// no need to check if it's already in dump because the value
				// must be the same if there is no preceding write
				mtd.Reads[hex.EncodeToString(a.Key)] = hex.EncodeToString(a.Value)
			case Has:
				if _, ok := writtenKey[string(a.Key)]; ok {
					continue
				}
				hasMap[hex.EncodeToString(a.Key)] = struct{}{}
			default:
				writtenKey[string(a.Key)] = struct{}{}
			}
		}
		for k := range hasMap {
			mtd.Has = append(mtd.Has, k)
		}
		d.Modules[name] = mtd
	}
	bz, err := json.Marshal(&d)
	if err != nil {
		panic(err)
	}
	return bz
}
