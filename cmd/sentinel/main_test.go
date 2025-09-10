package main

import "testing"

func TestScoreTxDeterministic(t *testing.T) {
	tx := "sample"
	if scoreTx(tx) != scoreTx(tx) {
		t.Fatal("scoreTx not deterministic")
	}
}

func TestPQSignDeterministic(t *testing.T) {
	pqKey = []byte("testkey")
	data := []byte("hello")
	sig1 := pqSign(data)
	sig2 := pqSign(data)
	if string(sig1) != string(sig2) {
		t.Fatal("pqSign not deterministic")
	}
}
