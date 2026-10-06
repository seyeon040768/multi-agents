package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/mattermost/mattermost/server/public/model"
)

// KVAPI is the subset of plugin.API needed for persistence.
type KVAPI interface {
	KVGet(string) ([]byte, *model.AppError)
	KVCompareAndSet(string, []byte, []byte) (bool, *model.AppError)
	KVCompareAndDelete(string, []byte) (bool, *model.AppError)
}
type KVAgentStore struct{ api KVAPI }

func NewKVAgentStore(api KVAPI) *KVAgentStore { return &KVAgentStore{api: api} }

const agentIndexKey = "agent:index:v1"

func agentKey(id string) string { return "agent:v1:" + id }

type agentIndex struct {
	IDs []string `json:"ids"`
}

func (s *KVAgentStore) read(id string) (*Agent, []byte, error) {
	raw, err := s.api.KVGet(agentKey(id))
	if err != nil {
		return nil, nil, err
	}
	if len(raw) == 0 {
		return nil, nil, ErrNotFound
	}
	var a Agent
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, nil, fmt.Errorf("decode agent: %w", err)
	}
	return &a, raw, nil
}
func (s *KVAgentStore) Get(id string) (*Agent, error) { a, _, err := s.read(id); return a, err }

// Index entries are retained after deletion. List skips missing records. This avoids
// a delayed delete removing the index entry of a concurrently recreated ID.
func (s *KVAgentStore) addIndex(id string) error {
	for i := 0; i < 32; i++ {
		old, e := s.api.KVGet(agentIndexKey)
		if e != nil {
			return e
		}
		idx := agentIndex{IDs: []string{}}
		if len(old) > 0 {
			if e := json.Unmarshal(old, &idx); e != nil {
				return e
			}
		}
		for _, existing := range idx.IDs {
			if existing == id {
				return nil
			}
		}
		idx.IDs = append(idx.IDs, id)
		sort.Strings(idx.IDs)
		raw, err := json.Marshal(idx)
		if err != nil {
			return err
		}
		ok, appErr := s.api.KVCompareAndSet(agentIndexKey, old, raw)
		if appErr != nil {
			return appErr
		}
		if ok {
			return nil
		}
	}
	return ErrConflict
}
func (s *KVAgentStore) Create(a *Agent) error {
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	// Reserve the index first: an interrupted create leaves only a harmless stale ID,
	// never a durable agent that cannot be discovered by List.
	if err := s.addIndex(a.ID); err != nil {
		return err
	}
	ok, e := s.api.KVCompareAndSet(agentKey(a.ID), nil, raw)
	if e != nil {
		return e
	}
	if !ok {
		return ErrAlreadyExists
	}
	return nil
}
func (s *KVAgentStore) List() ([]*Agent, error) {
	raw, e := s.api.KVGet(agentIndexKey)
	if e != nil {
		return nil, e
	}
	result := []*Agent{}
	if len(raw) == 0 {
		return result, nil
	}
	var idx agentIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, err
	}
	for _, id := range idx.IDs {
		a, err := s.Get(id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, nil
}
func (s *KVAgentStore) Update(a *Agent, expected int64) error {
	current, old, err := s.read(a.ID)
	if err != nil {
		return err
	}
	if current.Lifecycle.Version != expected {
		return ErrConflict
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	ok, e := s.api.KVCompareAndSet(agentKey(a.ID), old, raw)
	if e != nil {
		return e
	}
	if !ok {
		return ErrConflict
	}
	return nil
}
func (s *KVAgentStore) Delete(id string, expected int64) error {
	current, old, err := s.read(id)
	if err != nil {
		return err
	}
	if current.Lifecycle.Version != expected {
		return ErrConflict
	}
	ok, e := s.api.KVCompareAndDelete(agentKey(id), old)
	if e != nil {
		return e
	}
	if !ok {
		return ErrConflict
	}
	return nil
}

func botKey(userID string) string { return "agent:bot:v1:" + userID }
func (s *KVAgentStore) LinkBot(userID, id string) error {
	old, e := s.api.KVGet(botKey(userID))
	if e != nil {
		return e
	}
	if len(old) > 0 && string(old) != id {
		return ErrConflict
	}
	ok, e := s.api.KVCompareAndSet(botKey(userID), old, []byte(id))
	if e != nil {
		return e
	}
	if !ok {
		return ErrConflict
	}
	return nil
}
func (s *KVAgentStore) UnlinkBot(userID, id string) error {
	old, e := s.api.KVGet(botKey(userID))
	if e != nil {
		return e
	}
	if len(old) == 0 {
		return nil
	}
	if string(old) != id {
		return ErrConflict
	}
	ok, e := s.api.KVCompareAndDelete(botKey(userID), old)
	if e != nil {
		return e
	}
	if !ok {
		return ErrConflict
	}
	return nil
}
func (s *KVAgentStore) AgentIDForBot(userID string) (string, error) {
	raw, e := s.api.KVGet(botKey(userID))
	if e != nil {
		return "", e
	}
	if len(raw) == 0 {
		return "", ErrNotFound
	}
	return string(raw), nil
}
