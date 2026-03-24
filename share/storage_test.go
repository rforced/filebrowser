package share

import (
	"testing"
	"time"
)

type mockBackend struct {
	links   []*Link
	deleted []string
}

func (m *mockBackend) All() ([]*Link, error) {
	return m.links, nil
}

func (m *mockBackend) FindByUserID(_ uint) ([]*Link, error) {
	return m.links, nil
}

func (m *mockBackend) GetByHash(hash string) (*Link, error) {
	for _, l := range m.links {
		if l.Hash == hash {
			return l, nil
		}
	}
	return nil, nil
}

func (m *mockBackend) GetPermanent(_ string, _ uint) (*Link, error) {
	return nil, nil
}

func (m *mockBackend) Gets(_ string, _ uint) ([]*Link, error) {
	return m.links, nil
}

func (m *mockBackend) Save(_ *Link) error {
	return nil
}

func (m *mockBackend) Delete(hash string) error {
	m.deleted = append(m.deleted, hash)
	return nil
}

func (m *mockBackend) DeleteWithPathPrefix(_ string) error {
	return nil
}

func TestFilterExpired_SkipsValidLinksAfterExpired(t *testing.T) {
	now := time.Now().Unix()
	expired := now - 3600 // 1 hour ago
	valid := now + 3600   // 1 hour from now

	back := &mockBackend{
		links: []*Link{
			{Hash: "expired1", Path: "/a", Expire: expired, UserID: 1},
			{Hash: "valid1", Path: "/b", Expire: valid, UserID: 1},
			{Hash: "expired2", Path: "/c", Expire: expired, UserID: 1},
			{Hash: "valid2", Path: "/d", Expire: valid, UserID: 1},
		},
	}

	store := NewStorage(back)

	links, err := store.All()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(links) != 2 {
		t.Fatalf("expected 2 valid links, got %d", len(links))
	}

	if links[0].Hash != "valid1" {
		t.Errorf("expected first link hash 'valid1', got '%s'", links[0].Hash)
	}
	if links[1].Hash != "valid2" {
		t.Errorf("expected second link hash 'valid2', got '%s'", links[1].Hash)
	}

	if len(back.deleted) != 2 {
		t.Fatalf("expected 2 deletions, got %d", len(back.deleted))
	}
}

func TestFilterExpired_ConsecutiveExpiredLinks(t *testing.T) {
	now := time.Now().Unix()
	expired := now - 3600
	valid := now + 3600

	back := &mockBackend{
		links: []*Link{
			{Hash: "expired1", Path: "/a", Expire: expired, UserID: 1},
			{Hash: "expired2", Path: "/b", Expire: expired, UserID: 1},
			{Hash: "expired3", Path: "/c", Expire: expired, UserID: 1},
			{Hash: "valid1", Path: "/d", Expire: valid, UserID: 1},
		},
	}

	store := NewStorage(back)

	links, err := store.FindByUserID(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(links) != 1 {
		t.Fatalf("expected 1 valid link, got %d", len(links))
	}

	if links[0].Hash != "valid1" {
		t.Errorf("expected link hash 'valid1', got '%s'", links[0].Hash)
	}

	if len(back.deleted) != 3 {
		t.Fatalf("expected 3 deletions, got %d", len(back.deleted))
	}
}

func TestFilterExpired_PermanentLinksPreserved(t *testing.T) {
	now := time.Now().Unix()
	expired := now - 3600

	back := &mockBackend{
		links: []*Link{
			{Hash: "permanent", Path: "/a", Expire: 0, UserID: 1},
			{Hash: "expired1", Path: "/b", Expire: expired, UserID: 1},
			{Hash: "permanent2", Path: "/c", Expire: 0, UserID: 1},
		},
	}

	store := NewStorage(back)

	links, err := store.Gets("/a", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(links) != 2 {
		t.Fatalf("expected 2 permanent links, got %d", len(links))
	}

	if links[0].Hash != "permanent" || links[1].Hash != "permanent2" {
		t.Errorf("unexpected link hashes: %s, %s", links[0].Hash, links[1].Hash)
	}
}

func TestFilterExpired_EmptyList(t *testing.T) {
	back := &mockBackend{
		links: []*Link{},
	}

	store := NewStorage(back)

	links, err := store.All()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(links) != 0 {
		t.Fatalf("expected 0 links, got %d", len(links))
	}

	if len(back.deleted) != 0 {
		t.Fatalf("expected 0 deletions, got %d", len(back.deleted))
	}
}

func TestFilterExpired_AllExpired(t *testing.T) {
	expired := time.Now().Unix() - 3600

	back := &mockBackend{
		links: []*Link{
			{Hash: "expired1", Path: "/a", Expire: expired, UserID: 1},
			{Hash: "expired2", Path: "/b", Expire: expired, UserID: 1},
			{Hash: "expired3", Path: "/c", Expire: expired, UserID: 1},
		},
	}

	store := NewStorage(back)

	links, err := store.All()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if links == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}

	if len(links) != 0 {
		t.Fatalf("expected 0 links, got %d", len(links))
	}

	if len(back.deleted) != 3 {
		t.Fatalf("expected 3 deletions, got %d", len(back.deleted))
	}
}

func TestFilterExpired_AllValid(t *testing.T) {
	valid := time.Now().Unix() + 3600

	back := &mockBackend{
		links: []*Link{
			{Hash: "valid1", Path: "/a", Expire: valid, UserID: 1},
			{Hash: "valid2", Path: "/b", Expire: valid, UserID: 1},
			{Hash: "valid3", Path: "/c", Expire: valid, UserID: 1},
		},
	}

	store := NewStorage(back)

	links, err := store.FindByUserID(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(links) != 3 {
		t.Fatalf("expected 3 links, got %d", len(links))
	}

	if len(back.deleted) != 0 {
		t.Fatalf("expected 0 deletions, got %d", len(back.deleted))
	}
}

func TestFilterExpired_SingleExpired(t *testing.T) {
	expired := time.Now().Unix() - 3600

	back := &mockBackend{
		links: []*Link{
			{Hash: "expired1", Path: "/a", Expire: expired, UserID: 1},
		},
	}

	store := NewStorage(back)

	links, err := store.Gets("/a", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(links) != 0 {
		t.Fatalf("expected 0 links, got %d", len(links))
	}

	if len(back.deleted) != 1 {
		t.Fatalf("expected 1 deletion, got %d", len(back.deleted))
	}
}

func TestFilterExpired_SingleValid(t *testing.T) {
	valid := time.Now().Unix() + 3600

	back := &mockBackend{
		links: []*Link{
			{Hash: "valid1", Path: "/a", Expire: valid, UserID: 1},
		},
	}

	store := NewStorage(back)

	links, err := store.All()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}

	if links[0].Hash != "valid1" {
		t.Errorf("expected link hash 'valid1', got '%s'", links[0].Hash)
	}

	if len(back.deleted) != 0 {
		t.Fatalf("expected 0 deletions, got %d", len(back.deleted))
	}
}
