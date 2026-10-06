package agent

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeBots struct {
	created, updated, deleted, activated int
	active                               bool
	failure                              string
	info                                 *BotInfo
}

func (f *fakeBots) Create(a *Agent) (*BotInfo, error) {
	f.created++
	if f.failure == "create" {
		return nil, errors.New("bot creation failed")
	}
	f.info = &BotInfo{UserID: "bot-id", Username: "agent-" + a.ID}
	return f.info, nil
}
func (f *fakeBots) Update(a *Agent) (*BotInfo, error) {
	f.updated++
	if f.failure == "update" {
		return nil, errors.New("bot profile failed")
	}
	return f.info, nil
}
func (f *fakeBots) SetActive(_ string, active bool) error {
	f.activated++
	if f.failure == "active" {
		return errors.New("bot activation failed")
	}
	f.active = active
	return nil
}
func (f *fakeBots) Delete(string) error {
	f.deleted++
	if f.failure == "delete" {
		return errors.New("bot deletion failed")
	}
	f.info = nil
	return nil
}
func (f *fakeBots) Find(*Agent) (*BotInfo, error) { return f.info, nil }
func TestBotLifecycle(t *testing.T) {
	kv := newKV()
	store := NewKVAgentStore(kv)
	bots := &fakeBots{}
	service := NewServiceWithBots(store, bots, nil)
	a, err := service.Create(validAgent("researcher"))
	require.NoError(t, err)
	require.Equal(t, "ACTIVE", a.Runtime.Status)
	require.Equal(t, "bot-id", *a.Messenger.UserID)
	require.Equal(t, "agent-researcher", *a.Messenger.Username)
	require.True(t, bots.active)
	id, err := store.AgentIDForBot("bot-id")
	require.NoError(t, err)
	require.Equal(t, a.ID, id)
	// Restart preserves linkage without provisioning another bot.
	service = NewServiceWithBots(NewKVAgentStore(kv), bots, nil)
	a, err = service.Get(a.ID)
	require.NoError(t, err)
	require.Equal(t, "bot-id", *a.Messenger.UserID)
	calls := bots.updated
	version := a.Lifecycle.Version
	a.Prompts.Identity = "Changed prompt"
	a.Runtime.Status = "forged"
	forged := "forged"
	a.Messenger.UserID = &forged
	a, err = service.Update(a.ID, a, version)
	require.NoError(t, err)
	require.Equal(t, calls, bots.updated)
	require.Equal(t, "ACTIVE", a.Runtime.Status)
	require.Equal(t, "bot-id", *a.Messenger.UserID)
	a.DisplayName = "New display"
	a.Description = "New description"
	a, err = service.Update(a.ID, a, a.Lifecycle.Version)
	require.NoError(t, err)
	require.Equal(t, calls+1, bots.updated)
	oldVersion := a.Lifecycle.Version
	a, err = service.SetEnabled(a.ID, false, oldVersion)
	require.NoError(t, err)
	require.False(t, bots.active)
	require.Equal(t, "DISABLED", a.Runtime.Status)
	require.False(t, a.Lifecycle.Enabled)
	calls = bots.activated
	_, err = service.SetEnabled(a.ID, true, oldVersion)
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, calls, bots.activated)
	a, err = service.SetEnabled(a.ID, true, a.Lifecycle.Version)
	require.NoError(t, err)
	require.True(t, bots.active)
	require.Equal(t, "ACTIVE", a.Runtime.Status)
	require.NoError(t, service.Delete(a.ID, a.Lifecycle.Version))
	require.Nil(t, bots.info)
	_, err = store.AgentIDForBot("bot-id")
	require.ErrorIs(t, err, ErrNotFound)
	_, err = service.Get(a.ID)
	require.ErrorIs(t, err, ErrNotFound)
}
func TestBotFailuresAndRetry(t *testing.T) {
	for _, failure := range []string{"create", "update", "active", "delete"} {
		t.Run(failure, func(t *testing.T) {
			kv := newKV()
			bots := &fakeBots{}
			service := NewServiceWithBots(NewKVAgentStore(kv), bots, nil)
			if failure == "create" {
				bots.failure = failure
			}
			a, err := service.Create(validAgent("researcher"))
			require.NoError(t, err)
			if failure != "create" {
				bots.failure = failure
				switch failure {
				case "update":
					a.DisplayName = "Changed"
					a, err = service.Update(a.ID, a, a.Lifecycle.Version)
				case "active":
					a, err = service.SetEnabled(a.ID, false, a.Lifecycle.Version)
				case "delete":
					err = service.Delete(a.ID, a.Lifecycle.Version)
					require.Error(t, err)
					a, err = service.Get(a.ID)
				}
				require.NoError(t, err)
			}
			require.Equal(t, "ERROR", a.Runtime.Status)
			require.NotNil(t, a.Runtime.Error)
			stored, err := service.Get(a.ID)
			require.NoError(t, err)
			require.Equal(t, "ERROR", stored.Runtime.Status)
			bots.failure = ""
			if failure == "delete" {
				require.NoError(t, service.Delete(a.ID, a.Lifecycle.Version))
			} else {
				a, err = service.SetEnabled(a.ID, true, a.Lifecycle.Version)
				require.NoError(t, err)
				require.Equal(t, "ACTIVE", a.Runtime.Status)
				require.Nil(t, a.Runtime.Error)
			}
		})
	}
}
func TestBotCASFailurePreventsSideEffects(t *testing.T) {
	kv := newKV()
	bots := &fakeBots{}
	service := NewServiceWithBots(NewKVAgentStore(kv), bots, nil)
	a, err := service.Create(validAgent("researcher"))
	require.NoError(t, err)
	kv.reject = agentKey(a.ID)
	calls := bots.updated
	a.DisplayName = "Changed"
	_, err = service.Update(a.ID, a, a.Lifecycle.Version)
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, calls, bots.updated)
	require.ErrorIs(t, service.Delete(a.ID, a.Lifecycle.Version), ErrConflict)
	require.Zero(t, bots.deleted)
}
func TestInterruptedBotLinkCanBeDeleted(t *testing.T) {
	kv := newKV()
	store := NewKVAgentStore(kv)
	bots := &fakeBots{info: &BotInfo{UserID: "bot-id", Username: "agent-researcher"}}
	a := validAgent("researcher")
	a.Lifecycle.Version = 1
	a.Runtime.Status = "PROVISIONING"
	require.NoError(t, store.Create(a))
	require.NoError(t, NewServiceWithBots(store, bots, nil).Delete(a.ID, 1))
	require.Equal(t, 1, bots.deleted)
}
func TestBotMappingConflict(t *testing.T) {
	store := NewKVAgentStore(newKV())
	require.NoError(t, store.LinkBot("bot-id", "researcher"))
	require.ErrorIs(t, store.LinkBot("bot-id", "other"), ErrConflict)
	require.ErrorIs(t, store.UnlinkBot("bot-id", "other"), ErrConflict)
}
func TestConcurrentBotMutations(t *testing.T) {
	kv := newKV()
	bots := &fakeBots{}
	service := NewServiceWithBots(NewKVAgentStore(kv), bots, nil)
	a, err := service.Create(validAgent("researcher"))
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.SetEnabled(a.ID, false, a.Lifecycle.Version)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else {
			require.ErrorIs(t, err, ErrConflict)
		}
	}
	require.Equal(t, 1, success)
}

type failDeleteStore struct {
	*KVAgentStore
	fail bool
}

func (s *failDeleteStore) Delete(id string, version int64) error {
	if s.fail {
		return errors.New("KV delete unavailable")
	}
	return s.KVAgentStore.Delete(id, version)
}
func TestBotDeletedBeforeKVFailureCanRetry(t *testing.T) {
	store := &failDeleteStore{KVAgentStore: NewKVAgentStore(newKV())}
	bots := &fakeBots{}
	service := NewServiceWithBots(store, bots, nil)
	a, err := service.Create(validAgent("researcher"))
	require.NoError(t, err)
	store.fail = true
	require.Error(t, service.Delete(a.ID, a.Lifecycle.Version))
	require.Nil(t, bots.info)
	a, err = service.Get(a.ID)
	require.NoError(t, err)
	require.Equal(t, "DELETING", a.Runtime.Status)
	store.fail = false
	require.NoError(t, service.Delete(a.ID, a.Lifecycle.Version))
}
func TestReverseMappingFailurePersistsLinkForRetry(t *testing.T) {
	kv := newKV()
	store := NewKVAgentStore(kv)
	bots := &fakeBots{}
	kv.failure = botKey("bot-id")
	service := NewServiceWithBots(store, bots, nil)
	a, err := service.Create(validAgent("researcher"))
	require.NoError(t, err)
	require.Equal(t, "ERROR", a.Runtime.Status)
	require.Equal(t, "bot-id", *a.Messenger.UserID)
	kv.failure = ""
	a, err = service.SetEnabled(a.ID, true, a.Lifecycle.Version)
	require.NoError(t, err)
	require.Equal(t, "ACTIVE", a.Runtime.Status)
	require.Equal(t, 1, bots.created)
	id, err := store.AgentIDForBot("bot-id")
	require.NoError(t, err)
	require.Equal(t, a.ID, id)
}
