package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/linvva/cf-node-bench/internal/model"
)

func TestPinnedNodesPersistIndependentlyOfHistoryAndMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	candidate := model.Candidate{AddressType: model.AddressIPv4, IP: "192.0.2.1", Port: 443, Country: "US"}
	for _, mode := range []model.Mode{model.ModeTCPHTTPS, model.ModeH3, model.ModeTCPHTTPS} {
		if _, err := store.SetNodePinned(mode, candidate, true); err != nil {
			t.Fatal(err)
		}
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.PinnedNodes()) != 2 || len(reopened.History()) != 0 {
		t.Fatal("pins were lost, duplicated or tied to history")
	}
	nodes, err := reopened.SetNodePinned(model.ModeTCPHTTPS, candidate, false)
	if err != nil || len(nodes) != 1 || nodes[0].Mode != model.ModeH3 {
		t.Fatalf("unpin changed the other mode: %+v, %v", nodes, err)
	}
	nodes[0].Candidate.IP = "192.0.2.99"
	if reopened.PinnedNodes()[0].Candidate != candidate {
		t.Fatal("returned nodes mutate stored pins")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("pin settings permissions: %v, %v", info, err)
	}
}

func TestPinnedNodesRollBackWhenPersistenceFails(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	candidate := model.Candidate{IP: "192.0.2.1", Port: 443}
	if _, err := store.SetNodePinned(model.ModeTCPHTTPS, candidate, true); err != nil {
		t.Fatal(err)
	}
	store.path = t.TempDir() // Renaming a file over a directory must fail.
	if _, err := store.SetNodePinned(model.ModeTCPHTTPS, candidate, false); err == nil {
		t.Fatal("expected persistence failure")
	}
	if len(store.PinnedNodes()) != 1 {
		t.Fatal("failed save changed the in-memory configuration")
	}
}
